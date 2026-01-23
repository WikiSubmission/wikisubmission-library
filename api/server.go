package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth_gin"
	"github.com/gin-gonic/gin"
	"github.com/wikisubmission/ws-lib/api/handlers"
	"github.com/wikisubmission/ws-lib/aws"
	"github.com/wikisubmission/ws-lib/db"
	ginprometheus "github.com/zsais/go-gin-prometheus"
)

// StartServer initializes the HTTP server, sets up middleware,
// configures routes, and handles graceful shutdown.
func StartServer(database *db.DB) {
	// 1. Initialize CloudFront Signer
	signer, err := aws.LoadSigner()
	if err != nil {
		slog.Error("Failed to initialize CloudFront signer", slog.Any("error", err))
		os.Exit(1)
	}

	r := gin.Default()

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
	
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "alive"})
	})

	r.GET("/health", func(c *gin.Context) {
		// Deep health check: verify DB connectivity
		if err := database.Pool.Ping(c.Request.Context()); err != nil {
			slog.Error("Health check failed: DB unreachable", slog.Any("error", err))
			c.JSON(http.StatusInternalServerError, gin.H{"status": "unhealthy", "error": "db error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	// 5. Server Configuration
	port := os.Getenv("PORT")
	if port == "" {
    port = "8080" // fallback for local
}
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