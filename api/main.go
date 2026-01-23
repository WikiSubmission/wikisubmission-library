// Package main is the entry point for the WikiSubmission Library API.
// It handles dependency injection, background worker orchestration, and server startup.
package main

import (
	"log/slog"
	"os"

	"github.com/joho/godotenv"
	"github.com/wikisubmission/ws-lib/aws"
	"github.com/wikisubmission/ws-lib/db"
)

func main() {
	// 1. Initialize Structured Logging (JSON)
	// Using slog.NewJSONHandler makes logs machine-readable for tools like Grafana Loki or Datadog.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// 2. Load Environment Variables
	// godotenv loads variables from a .env file into the system environment.
	if err := godotenv.Load(); err != nil {
		slog.Info("No .env file found, using system environment variables")
	}

	// 3. Initialize Database and run Migrations
	// DbSetup handles the connection pool and executes SQL scripts in db/migrations.
	database, err := db.DbSetup()
	if err != nil {
		slog.Error("Database setup failed", slog.Any("error", err))
		os.Exit(1)
	}
	defer database.Close()

	// 4. Initialize AWS SDK
	// Loads configuration for S3 (syncing) and SQS (event listening).
	s3Client, sqsClient, err := aws.InitAWSConfig()
	if err != nil {
		slog.Error("AWS initialization failed", slog.Any("error", err))
		os.Exit(1)
	}

	// 5. Start the SQS Worker
	// This worker runs in a separate goroutine to process real-time S3 events (Put/Delete).
	queueURL := os.Getenv("SQS_QUEUE_URL")
	if queueURL == "" {
		slog.Error("Startup failed: SQS_QUEUE_URL environment variable is required")
		os.Exit(1)
	}

	slog.Info("Starting SQS Worker", "queue_url", queueURL)
	go database.RunSQSWorker(sqsClient, queueURL)

	// 6. Run Initial S3 to Database Sync
	// Ensures the database stays consistent with S3 bucket state after downtime.
	bucket := os.Getenv("BUCKET_NAME")
	if bucket == "" {
		slog.Warn("BUCKET_NAME not set; initial sync will be skipped")
	} else {
		slog.Info("Starting initial S3 sync", "bucket", bucket)
		go database.StartSync(s3Client, bucket)
	}

	// 7. Start the HTTP Server
	// This blocks the main thread. Graceful shutdown is handled inside StartServer.
	slog.Info("Starting API Server", "port", 8081)
	StartServer(database)
}