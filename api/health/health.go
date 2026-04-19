// Package health implements the wikisubmission health-endpoint contract.
// See wikisubmission-infra/runbooks/health-endpoints.md for the full spec.
//
// Exposes three Gin handlers:
//
//	Liveness  — "/healthz"           — cheap, no dependency calls
//	Readiness — "/healthz/ready"     — runs critical checks
//	Detailed  — "/healthz/detailed"  — requires HMAC signature, returns rich payload
//
// And one middleware:
//
//	RequireSignature(secrets) — verifies X-Health-Signature / X-Health-Timestamp
package health

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Build-time metadata. Set via linker flags:
//
//	go build -ldflags "-X .../api/health.Version=$(git describe --tags) \
//	                   -X .../api/health.GitSHA=$(git rev-parse --short HEAD) \
//	                   -X .../api/health.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// Left as package-level vars (not constants) so they can be ldflag-overridden.
var (
	Version   = "dev"
	GitSHA    = "unknown"
	BuildTime = "unknown"
)

// Check is one dependency probe. Fn should return nil on success. Critical
// checks gate Readiness — non-critical only appear in Detailed.
type Check struct {
	Name     string
	Critical bool
	Fn       func(ctx context.Context) error
}

// Deps bundles everything a service needs to expose health. All fields are
// optional except Service and Checks.
type Deps struct {
	Service   string
	Checks    []Check
	Env       map[string]any
	startedAt time.Time
}

// New returns a Deps with startedAt pinned to now. Use functional options to
// populate the rest.
func New(service string, opts ...Option) *Deps {
	d := &Deps{
		Service:   service,
		startedAt: time.Now().UTC(),
		Env:       map[string]any{},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

type Option func(*Deps)

func WithCheck(name string, critical bool, fn func(ctx context.Context) error) Option {
	return func(d *Deps) {
		d.Checks = append(d.Checks, Check{Name: name, Critical: critical, Fn: fn})
	}
}

func WithEnv(key string, value any) Option {
	return func(d *Deps) { d.Env[key] = value }
}

// Liveness is the cheap probe. 200 always, unless the process is about to die.
func (d *Deps) Liveness() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "service": d.Service})
	}
}

// Readiness runs every critical check. 200 when all pass, 503 otherwise.
// Bounded by a 2s timeout so a slow downstream can't wedge the probe.
func (d *Deps) Readiness() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		results := runChecks(ctx, d.Checks)
		status := aggregateStatus(results, d.Checks)
		code := http.StatusOK
		if status == "unhealthy" {
			code = http.StatusServiceUnavailable
		}
		c.JSON(code, gin.H{"status": status, "checks": results})
	}
}

// Detailed emits the full diagnostic payload. Must be behind RequireSignature.
func (d *Deps) Detailed() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		results := runChecks(ctx, d.Checks)
		status := aggregateStatus(results, d.Checks)

		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)

		hostname, _ := hostnameOrUnknown()
		now := time.Now().UTC()

		c.JSON(http.StatusOK, gin.H{
			"service":        d.Service,
			"status":         status,
			"version":        Version,
			"git_sha":        GitSHA,
			"build_time":     BuildTime,
			"started_at":     d.startedAt.Format(time.RFC3339),
			"uptime_seconds": int64(now.Sub(d.startedAt).Seconds()),
			"now":            now.Format(time.RFC3339),
			"host": gin.H{
				"hostname":     hostname,
				"cpu_count":    runtime.NumCPU(),
				"memory_bytes": mem.Alloc,
				"goroutines":   runtime.NumGoroutine(),
			},
			"checks": results,
			"env":    d.Env,
		})
	}
}

// RequireSignature returns a middleware that rejects requests missing a valid
// HMAC signature bound to the current wall-clock time.
//
// secretsFn returns one or more accepted secrets. Returning multiple values
// supports a rotation window (current + previous). The middleware aborts with
// 401 if none match.
func RequireSignature(secretsFn func() [][]byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		ts := c.GetHeader("X-Health-Timestamp")
		sig := c.GetHeader("X-Health-Signature")
		if ts == "" || sig == "" {
			abort(c, http.StatusUnauthorized, "missing X-Health-Timestamp or X-Health-Signature")
			return
		}

		unix, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			abort(c, http.StatusUnauthorized, "invalid X-Health-Timestamp")
			return
		}
		skew := time.Since(time.Unix(unix, 0))
		if skew < 0 {
			skew = -skew
		}
		if skew > 30*time.Second {
			abort(c, http.StatusUnauthorized, "timestamp drift exceeds 30s")
			return
		}

		provided, ok := parseSignature(sig)
		if !ok {
			abort(c, http.StatusUnauthorized, "malformed signature")
			return
		}

		message := c.Request.Method + "\n" + c.Request.URL.Path + "\n" + ts
		for _, secret := range secretsFn() {
			if len(secret) == 0 {
				continue
			}
			expected := computeHMAC(secret, message)
			if subtle.ConstantTimeCompare(provided, expected) == 1 {
				c.Next()
				return
			}
		}
		abort(c, http.StatusUnauthorized, "signature mismatch")
	}
}

// Register wires all three endpoints onto the given engine at the standard paths.
// The middleware is applied to /healthz/detailed only.
func (d *Deps) Register(r *gin.Engine, secretsFn func() [][]byte) {
	r.GET("/healthz", d.Liveness())
	r.GET("/healthz/ready", d.Readiness())
	r.GET("/healthz/detailed", RequireSignature(secretsFn), d.Detailed())
}

// ── internals ──────────────────────────────────────────────────────────────

type checkResult struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
	Error     string `json:"error,omitempty"`
	Critical  bool   `json:"critical"`
}

func runChecks(ctx context.Context, checks []Check) []checkResult {
	results := make([]checkResult, len(checks))
	var wg sync.WaitGroup
	for i, check := range checks {
		wg.Add(1)
		go func(idx int, ch Check) {
			defer wg.Done()
			start := time.Now()
			err := ch.Fn(ctx)
			res := checkResult{
				Name:      ch.Name,
				LatencyMS: time.Since(start).Milliseconds(),
				Critical:  ch.Critical,
			}
			if err != nil {
				res.Status = "fail"
				res.Error = err.Error()
			} else {
				res.Status = "ok"
			}
			results[idx] = res
		}(i, check)
	}
	wg.Wait()
	return results
}

func aggregateStatus(results []checkResult, checks []Check) string {
	anyCriticalFail := false
	anyNonCriticalFail := false
	for _, r := range results {
		if r.Status == "ok" {
			continue
		}
		if r.Critical {
			anyCriticalFail = true
		} else {
			anyNonCriticalFail = true
		}
	}
	switch {
	case anyCriticalFail:
		return "unhealthy"
	case anyNonCriticalFail:
		return "degraded"
	default:
		return "ok"
	}
}

func computeHMAC(secret []byte, message string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}

// parseSignature accepts either "v1=<hex>" or raw hex for forward-compat with
// non-versioned callers. Returns the decoded bytes.
func parseSignature(sig string) ([]byte, bool) {
	if idx := strings.Index(sig, "="); idx >= 0 {
		version := sig[:idx]
		if version != "v1" {
			return nil, false
		}
		sig = sig[idx+1:]
	}
	b, err := hex.DecodeString(sig)
	if err != nil {
		return nil, false
	}
	return b, true
}

func abort(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"error": msg})
}

func hostnameOrUnknown() (string, error) {
	n, err := os.Hostname()
	if err != nil {
		return "unknown", err
	}
	return n, nil
}
