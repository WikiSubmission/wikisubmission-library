package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// SearchObjects performs a fuzzy search on the s3_objects table using GIN trigram indexing.
// It returns a slice of S3Object sorted by their similarity score to the searchTerm.
//
// The similarity threshold is determined by the PostgreSQL 'pg_trgm.similarity_threshold' setting,
// which can be adjusted globally or per-session.
func (db *DB) SearchObjects(ctx context.Context, searchTerm string, limit int) ([]S3Object, error) {
	start := time.Now()

	// The '%' operator uses the GIN trigram index for high-performance fuzzy matching.
	// similarity() calculates a float score from 0.0 to 1.0 for ranking relevance.
	query := `
        SELECT file_key, file_size, last_modified, similarity(file_key, $1) as sml
        FROM s3_objects
        WHERE file_key % $1
        ORDER BY sml DESC
        LIMIT $2;
    `

	rows, err := db.Pool.Query(ctx, query, searchTerm, limit)
	if err != nil {
		slog.Error("Database search query failed", 
			slog.String("term", searchTerm), 
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("search query failed: %w", err)
	}
	defer rows.Close()

	// Initialize as empty slice rather than nil to ensure valid JSON [] output
	results := []S3Object{}

	for rows.Next() {
		var obj S3Object
		var lastMod time.Time
		
		err := rows.Scan(&obj.FileKey, &obj.FileSize, &lastMod, &obj.Similarity)
		if err != nil {
			slog.Error("Failed to scan search result row", slog.Any("error", err))
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		
		obj.LastModified = lastMod.Format(time.RFC3339)
		results = append(results, obj)
	}

	if err := rows.Err(); err != nil {
		slog.Error("Post-processing error during row iteration", slog.Any("error", err))
		return nil, err
	}

	// Structured log for search performance analytics
	slog.Info("Search execution completed",
		slog.String("query", searchTerm),
		slog.Int("limit", limit),
		slog.Int("results_found", len(results)),
		slog.Duration("latency", time.Since(start)),
	)

	return results, nil
}

// SetTrgmStrictness adjusts the PostgreSQL trigram similarity threshold for the current connection.
// 'val' should be a value between 0 and 100 (where PostgreSQL expects a 0.0 to 1.0 float).
// Lower values make the search "fuzzier" (less strict), while higher values require closer matches.
func (db *DB) SetTrgmStrictness(ctx context.Context, val float64) error {
	// Using a float64 is more standard for trigram thresholds (e.g., 0.3)
	_, err := db.Pool.Exec(ctx, fmt.Sprintf("SET pg_trgm.similarity_threshold = %v", val))
	if err != nil {
		slog.Error("Failed to set trigram similarity threshold", 
			slog.Float64("value", val), 
			slog.Any("error", err),
		)
		return err
	}
	
	slog.Info("Similarity threshold updated", slog.Float64("new_threshold", val))
	return nil
}