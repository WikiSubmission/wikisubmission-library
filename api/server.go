package main

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth_gin"
	"github.com/gin-gonic/gin"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/wikisubmission/ws-lib/api/handlers"
	"github.com/wikisubmission/ws-lib/aws"
	"github.com/wikisubmission/ws-lib/db"
	ginprometheus "github.com/zsais/go-gin-prometheus"
)

//go:embed templates/*.html
var templateFS embed.FS

// StartServer initializes the HTTP server, sets up middleware,
// configures routes, and handles graceful shutdown.
func StartServer(database *db.DB, s3Client *s3sdk.Client, bucket string) {
	// 1. Initialize CloudFront Signer
	signer, err := aws.LoadSigner()
	if err != nil {
		slog.Error("Failed to initialize CloudFront signer", slog.Any("error", err))
		os.Exit(1)
	}

	r := gin.Default()
	r.Use(SupportHEAD)

	funcMap := template.FuncMap{
		"lastPathExtension": func(filename string) string {
			ext := filepath.Ext(filename)
			return strings.TrimPrefix(strings.ToLower(ext), ".")
		},
		"formatBytes": func(b int64) string {
			const unit = 1024
			if b < unit { return fmt.Sprintf("%d B", b) }
			div, exp := int64(unit), 0
			for n := b / unit; n >= unit; n /= unit {
				div *= unit
				exp++
			}
			return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
		},
	}
	templ := template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html"))
    
    r.SetHTMLTemplate(templ)	
	// Configure Trusted Proxies from Env
	proxiesEnv := os.Getenv("TRUSTED_PROXIES")
	if proxiesEnv != "" {
		proxyList := strings.Split(proxiesEnv, ",")
		r.SetTrustedProxies(proxyList)
		slog.Info("Trusted proxies configured", "proxies", proxyList)
	} else {
		r.SetTrustedProxies(nil)
		slog.Warn("No trusted proxies set; trusting all (standard Gin default)")
	}

	// Prometheus Exporter
	p := ginprometheus.NewPrometheus("gin")
	p.Use(r)

	// 3. Rate Limiting (5 req/sec per IP)
	limiter := tollbooth.NewLimiter(5, nil)

	// 4. API Routes
	r.GET("/search", tollbooth_gin.LimitHandler(limiter), handlers.SearchHandler(database, signer))
	r.GET("/filename/*filepath", handlers.FileNameHandler(database, signer))
	r.GET("/file/*filepath", tollbooth_gin.LimitHandler(limiter), handlers.FileHandler(database, signer))
	r.GET("/explorer", handlers.ExplorerHandler(database))
	
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "alive"})
	})

	r.GET("/favicon.ico", func (c *gin.Context)  {
		logo_key := "wikisubmission/media/images/logo.png"
		signer.GetURL(logo_key, time.Hour)
	})

	r.GET("/health", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		checks := gin.H{}
		healthy := true

		// DB check
		if err := database.Pool.Ping(ctx); err != nil {
			slog.Error("Health check: DB unreachable", slog.Any("error", err))
			checks["db"] = "error: " + err.Error()
			healthy = false
		} else {
			checks["db"] = "ok"
		}

		// S3 check — verifies AWS credentials are valid (critical after key rotation)
		if bucket == "" {
			checks["s3"] = "skipped: BUCKET_NAME not set"
		} else if err := aws.CheckBucketAccess(ctx, s3Client, bucket); err != nil {
			slog.Error("Health check: S3 unreachable", slog.Any("error", err))
			checks["s3"] = "error: " + err.Error()
			healthy = false
		} else {
			checks["s3"] = "ok"
		}

		if healthy {
			c.JSON(http.StatusOK, gin.H{"status": "healthy", "checks": checks})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"status": "unhealthy", "checks": checks})
		}
	})
	r.GET("/", handlers.IndexHandler())
	r.GET("/robots.txt", func(c *gin.Context) {
			const robots = `User-agent: *
		Allow: /
		Allow: /explorer
		Disallow: /file/
		Disallow: /search
		Disallow: /api/
		Disallow: /file/private/`

			c.String(200, robots)
		})
	
	r.GET("/sitemap.xml", func(c *gin.Context) {
		c.Header("Content-Type", "application/xml")
		c.Header("Cache-Control", "public, max-age=86400") 	

		sitemap := `<?xml version="1.0" encoding="UTF-8"?>
	<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
		<url>
			<loc>https://library.wikisubmission.org/</loc>
			<changefreq>daily</changefreq>
			<priority>1.0</priority>
		</url>
		<url>
			<loc>https://library.wikisubmission.org/explorer</loc>
			<changefreq>hourly</changefreq>
			<priority>0.8</priority>
		</url>
	</urlset>`

		c.String(http.StatusOK, sitemap)
	})

	// 5. Server Configuration
	port := os.Getenv("PORT")
		if port == "" {
			port = "8080" 
		}
	slog.Info("Starting API Server", "port", port)
	srv := &http.Server{
		Addr:    fmt.Sprintf(":%v", port),
		Handler: r,
	}

	// 6. Graceful Shutdown Logic
	// Start server in a goroutine so it doesn't block the shutdown signal listener
	go func() {
		slog.Info("API Server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server listen error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// Channel to listen for interrupt signals (SIGINT, SIGTERM)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	
	<-quit // Block here until a signal is received
	slog.Info("Shutdown signal received; shutting down gracefully...")

	// Create a deadline for the shutdown process
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", slog.Any("error", err))
	}

	slog.Info("Server exited cleanly")
}