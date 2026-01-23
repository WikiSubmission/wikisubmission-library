package db

import (
	"context"
	"testing"

	"github.com/joho/godotenv"
)
func TestParseAndProcessEvents(t *testing.T) {
    _ = godotenv.Load()
    
    // You MUST initialize the DB properly so the Pool isn't nil
    database, err := DbSetup()
    if err != nil {
        t.Skip("Skipping: DB not available")
    }
    defer database.Close()

    mockBody := `{
      "Records": [
        {
          "eventName": "ObjectCreated:Put",
          "eventTime": "2024-01-01T00:00:00Z",
          "s3": {
            "object": {
              "key": "test/path/item.wav",
              "size": 1234,
              "eTag": "abcde"
            }
          }
        }
      ]
    }`

    t.Run("Event Logic Branching", func(t *testing.T) {
        // Use 'database' (which has a pool), not an empty '&DB{}'
        err := database.ProcessS3Events(context.Background(), mockBody)
        if err != nil {
            t.Errorf("Expected success, got error: %v", err)
        }
    })
}