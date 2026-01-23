package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)


type SearchOption func(*searchConfig)

type searchConfig struct {
    threshold float64
}

func WithThreshold(t float64) SearchOption {
    return func(c *searchConfig) {
        c.threshold = t
    }
}

// SearchObjects performs a fuzzy search on the s3_objects table using GiST trigram indexing.
// It uses the distance operator <-> for KNN (K-Nearest Neighbor) sorting, which is
// significantly faster than similarity() for large datasets.
func (db *DB) SearchObjects(ctx context.Context, searchTerm string, limit int, opts ...SearchOption) ([]S3Object, error) {
	start := time.Now()

	// Default threshold if not provided
	config := &searchConfig{threshold: 0.15}
    
    // Apply overrides
    for _, opt := range opts {
        opt(config)
    }

	// Initialize as empty slice to ensure valid JSON [] output
	results := []S3Object{}

	// Use a transaction to ensure SET LOCAL only affects this specific request
	conn, err := db.Pool.Acquire(ctx)
    if err != nil {
        return nil, err
    }
    defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Set the local threshold for this transaction
	_, err = tx.Exec(ctx, fmt.Sprintf("SET LOCAL pg_trgm.similarity_threshold = %v", config.threshold))
	if err != nil {
		return nil, fmt.Errorf("failed to set local threshold: %w", err)
	}

	// 2. Execute the optimized GiST KNN query
	// (1 - (file_key <-> $1)) converts 'distance' back into a 'similarity score' for your frontend
	query := `
        SELECT id, file_key, file_size, last_modified, etag, 
               (1 - (file_key <-> $1)) as score
        FROM s3_objects
        WHERE file_key % $1
        ORDER BY file_key <-> $1
        LIMIT $2;
    `

	rows, err := tx.Query(ctx, query, searchTerm, limit)
	if err != nil {
		slog.Error("Database search query failed",
			slog.String("term", searchTerm),
			slog.Any("error", err),
		)
		return nil, fmt.Errorf("search query failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var obj S3Object
		var lastMod time.Time

		// Scanning all necessary data based on your table structure
		err := rows.Scan(&obj.ID, &obj.FileKey, &obj.FileSize, &lastMod, &obj.ETag, &obj.Similarity)
		if err != nil {
			slog.Error("Failed to scan search result row", slog.Any("error", err))
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		obj.LastModified = lastMod.Format(time.RFC3339)
		results = append(results, obj)
	}

	// Commit the transaction (though LOCAL changes discard anyway on close)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	slog.Info("Search execution completed",
		slog.String("query", searchTerm),
		slog.Float64("threshold", config.threshold),
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


// GetObjectByKey retrieves a single S3 object from the database using an exact match on the file_key.
//
// Performance:
// This method performs an O(log n) lookup using the standard B-tree index (idx_s3_objects_key_btree).
// It is significantly faster than the trigram search and should be used for direct file access
// via permanent links or specific path requests.
//
// Returns:
// - (*S3Object, nil) if a match is found.
// - (nil, pgx.ErrNoRows) if no exact match exists.
// - (nil, error) for database connection or scanning issues.
func (db *DB) GetObjectByKey(ctx context.Context, key string) (*S3Object, error) {
    // We use a LIMIT 1 as an optimization, though file_key should be unique.
    query := `
        SELECT id, file_key, file_size, last_modified, etag 
        FROM s3_objects 
        WHERE file_key = $1 
        LIMIT 1;
    `
    var obj S3Object
    var lastMod time.Time

    // QueryRow is used here because we expect exactly one (or zero) results.
    err := db.Pool.QueryRow(ctx, query, key).Scan(
        &obj.ID, &obj.FileKey, &obj.FileSize, &lastMod, &obj.ETag,
    )
    if err != nil {
        return nil, err
    }

    // Format the time.Time into an RFC3339 string for JSON consistency across the API.
    obj.LastModified = lastMod.Format(time.RFC3339)
    return &obj, nil
}

// GetAllObjects retrieves every file in the database. 
// It is optimized for the initial "Cache Miss" scenario where we need to build the full explorer tree.
// Unlike SearchObjects, it does not calculate similarity scores.
func (db *DB) GetAllObjects(ctx context.Context) ([]S3Object, error) {
    start := time.Now()

    // Initialize as empty slice so JSON returns [] instead of null if empty
    results := []S3Object{}

    // We don't need a transaction or custom threshold settings here,
    // just a standard connection acquisition.
    conn, err := db.Pool.Acquire(ctx)
    if err != nil {
        return nil, err
    }
    defer conn.Release()

    // Simple SELECT, ordered alphabetically to make the resulting tree deterministic
    query := `
        SELECT id, file_key, file_size, last_modified, etag
        FROM s3_objects
        ORDER BY file_key ASC;
    `

    rows, err := conn.Query(ctx, query)
    if err != nil {
        slog.Error("Failed to fetch all objects", slog.Any("error", err))
        return nil, fmt.Errorf("query failed: %w", err)
    }
    defer rows.Close()

    for rows.Next() {
        var obj S3Object
        var lastMod time.Time

        // Note: We do NOT scan 'Similarity' here because it doesn't exist in this query.
        // It will default to 0.0 in the struct, which is correct for a non-search view.
        err := rows.Scan(&obj.ID, &obj.FileKey, &obj.FileSize, &lastMod, &obj.ETag)
        if err != nil {
            slog.Error("Failed to scan object row", slog.Any("error", err))
            return nil, fmt.Errorf("failed to scan row: %w", err)
        }

        obj.LastModified = lastMod.Format(time.RFC3339)
        results = append(results, obj)
    }

    // Check for errors that occurred during iteration
    if err := rows.Err(); err != nil {
        return nil, fmt.Errorf("row iteration error: %w", err)
    }

    slog.Info("Fetched all objects",
        slog.Int("count", len(results)),
        slog.Duration("latency", time.Since(start)),
    )

    return results, nil
}