package health

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func sign(secret []byte, method, path string, ts int64) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(fmt.Sprintf("%s\n%s\n%d", method, path, ts)))
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func do(t *testing.T, r http.Handler, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestLiveness_AlwaysOK(t *testing.T) {
	d := New("svc")
	r := gin.New()
	r.GET("/healthz", d.Liveness())

	w := do(t, r, "GET", "/healthz", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestReadiness_AggregateStatus(t *testing.T) {
	tests := []struct {
		name     string
		checks   []Check
		wantCode int
		wantSt   string
	}{
		{
			name:     "all pass",
			checks:   []Check{{Name: "db", Critical: true, Fn: func(context.Context) error { return nil }}},
			wantCode: http.StatusOK,
			wantSt:   "ok",
		},
		{
			name:     "critical fail → unhealthy",
			checks:   []Check{{Name: "db", Critical: true, Fn: func(context.Context) error { return errors.New("boom") }}},
			wantCode: http.StatusServiceUnavailable,
			wantSt:   "unhealthy",
		},
		{
			name: "non-critical fail → degraded, still 200",
			checks: []Check{
				{Name: "db", Critical: true, Fn: func(context.Context) error { return nil }},
				{Name: "cache", Critical: false, Fn: func(context.Context) error { return errors.New("miss") }},
			},
			wantCode: http.StatusOK,
			wantSt:   "degraded",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Deps{Service: "svc", Checks: tt.checks, startedAt: time.Now()}
			r := gin.New()
			r.GET("/healthz/ready", d.Readiness())

			w := do(t, r, "GET", "/healthz/ready", nil)
			if w.Code != tt.wantCode {
				t.Fatalf("code: want %d got %d; body=%s", tt.wantCode, w.Code, w.Body.String())
			}
			var body struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Status != tt.wantSt {
				t.Fatalf("status: want %s got %s", tt.wantSt, body.Status)
			}
		})
	}
}

func TestRequireSignature(t *testing.T) {
	secret := []byte("shared-secret-xyz")
	secretsFn := func() [][]byte { return [][]byte{secret} }

	r := gin.New()
	r.GET("/healthz/detailed", RequireSignature(secretsFn), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	now := time.Now().Unix()

	t.Run("missing headers → 401", func(t *testing.T) {
		w := do(t, r, "GET", "/healthz/detailed", nil)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d", w.Code)
		}
	})

	t.Run("valid signature → 200", func(t *testing.T) {
		w := do(t, r, "GET", "/healthz/detailed", map[string]string{
			"X-Health-Timestamp": strconv.FormatInt(now, 10),
			"X-Health-Signature": sign(secret, "GET", "/healthz/detailed", now),
		})
		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("tampered path → 401", func(t *testing.T) {
		w := do(t, r, "GET", "/healthz/detailed", map[string]string{
			"X-Health-Timestamp": strconv.FormatInt(now, 10),
			"X-Health-Signature": sign(secret, "GET", "/other/path", now),
		})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d", w.Code)
		}
	})

	t.Run("old timestamp → 401", func(t *testing.T) {
		old := now - 120
		w := do(t, r, "GET", "/healthz/detailed", map[string]string{
			"X-Health-Timestamp": strconv.FormatInt(old, 10),
			"X-Health-Signature": sign(secret, "GET", "/healthz/detailed", old),
		})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d", w.Code)
		}
	})

	t.Run("wrong secret → 401", func(t *testing.T) {
		w := do(t, r, "GET", "/healthz/detailed", map[string]string{
			"X-Health-Timestamp": strconv.FormatInt(now, 10),
			"X-Health-Signature": sign([]byte("nope"), "GET", "/healthz/detailed", now),
		})
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("want 401, got %d", w.Code)
		}
	})

	t.Run("rotation window accepts previous secret", func(t *testing.T) {
		current := []byte("new-secret")
		previous := []byte("old-secret")
		rotating := func() [][]byte { return [][]byte{current, previous} }

		r2 := gin.New()
		r2.GET("/healthz/detailed", RequireSignature(rotating), func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

		// Dashboard still signing with previous during the rollout window.
		w := do(t, r2, "GET", "/healthz/detailed", map[string]string{
			"X-Health-Timestamp": strconv.FormatInt(now, 10),
			"X-Health-Signature": sign(previous, "GET", "/healthz/detailed", now),
		})
		if w.Code != http.StatusOK {
			t.Fatalf("want 200 accepting previous secret, got %d", w.Code)
		}
	})
}

func TestParseSignature(t *testing.T) {
	t.Run("v1 prefix", func(t *testing.T) {
		b, ok := parseSignature("v1=deadbeef")
		if !ok || hex.EncodeToString(b) != "deadbeef" {
			t.Fatalf("parse v1 failed: %v %v", b, ok)
		}
	})
	t.Run("raw hex", func(t *testing.T) {
		b, ok := parseSignature("deadbeef")
		if !ok || hex.EncodeToString(b) != "deadbeef" {
			t.Fatalf("parse raw failed: %v %v", b, ok)
		}
	})
	t.Run("unknown version", func(t *testing.T) {
		if _, ok := parseSignature("v99=deadbeef"); ok {
			t.Fatal("v99 should be rejected")
		}
	})
	t.Run("not hex", func(t *testing.T) {
		if _, ok := parseSignature("v1=notvalidhex"); ok {
			t.Fatal("non-hex should be rejected")
		}
	})
}
