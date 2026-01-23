package db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

// TestFullLifecycle verifies connection, migration, upsert, and search logic.
func TestFullLifecycle(t *testing.T) {
	_ = godotenv.Load() 
	if os.Getenv("DATABASE_NAME") == "" {
		t.Skip("Skipping test: No DB env vars found")
	}

	// 1. Setup (Connects + Migrates)
	database, err := DbSetup()
	if err != nil {
		t.Fatalf("DbSetup failed: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	t.Run("Upsert and Delete Lifecycle", func(t *testing.T) {
		testObj := S3Object{
			FileKey:      "tests/audio_sample.mp3",
			FileSize:     9999,
			LastModified: time.Now().Format(time.RFC3339),
			ETag:         "test-tag-123",
		}

		// Test Upsert
		count, err := database.UpsertS3Objects(ctx, []S3Object{testObj})
		if err != nil || count == 0 {
			t.Errorf("Failed to upsert: %v", err)
		}
		t.Log("Upsert successful")

		// Test Search (Fuzzy)
		results, err := database.SearchObjects(ctx, "audio sampl", 1) // intentional typo
		if err != nil || len(results) == 0 {
			t.Errorf("Fuzzy search failed: %v", err)
		} else {
			t.Logf("Search match found: %s (Similarity: %f)", results[0].FileKey, results[0].Similarity)
		}

		// Test Delete
		err = database.DeleteS3Object(ctx, testObj.FileKey)
		if err != nil {
			t.Errorf("Delete failed: %v", err)
		}
		t.Log("Delete successful")
	})
}