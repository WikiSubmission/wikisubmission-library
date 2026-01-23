package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5"
)

// StartSync acts as a concurrency-safe wrapper around the batch sync process.
// It uses an atomic swap to ensure that only one synchronization process
// runs at any given time to prevent database thrashing and redundant S3 API calls.
func (db *DB) StartSync(s3Client *s3.Client, bucket string) {
	// Attempt to set isSyncing to true; if it was already true, exit.
	if db.isSyncing.Swap(true) {
		slog.Warn("Sync already in progress, skipping duplicate request", "bucket", bucket)
		return
	}

	// Ensure the flag is reset to false when the function finishes.
	defer db.isSyncing.Store(false)

	start := time.Now()
	slog.Info("Starting S3 to DB sync", "bucket", bucket)

	count, err := db.InitialSyncS3ToPostgresBatch(s3Client, bucket)
	if err != nil {
		slog.Error("Initial sync failed", 
			slog.String("bucket", bucket), 
			slog.Any("error", err),
			slog.Duration("elapsed", time.Since(start)),
		)
		return
	}

	slog.Info("Sync completed successfully", 
		slog.String("bucket", bucket), 
		slog.Int("processed_objects", count), 
		slog.Duration("duration", time.Since(start)),
	)
}

// InitialSyncS3ToPostgresBatch performs a full crawl of the specified S3 bucket
// and mirrors the object metadata into the PostgreSQL database using batched inserts.
//
// It utilizes S3 pagination to handle buckets with tens of thousands of objects and
// pgx.Batch to minimize network round-trips to the database.
func (db *DB) InitialSyncS3ToPostgresBatch(s3Client *s3.Client, bucketName string) (int, error) {
	// Use ListObjectsV2 paginator to handle > 1000 objects automatically
	paginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})

	count := 0
	ctx := context.Background()

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return count, fmt.Errorf("failed to get page from S3: %w", err)
		}

		batch := &pgx.Batch{}
		for _, obj := range page.Contents {
			query := `
                INSERT INTO s3_objects (file_key, file_size, last_modified, etag)
                VALUES ($1, $2, $3, $4)
                ON CONFLICT (file_key) DO UPDATE SET
                    file_size = EXCLUDED.file_size,
                    last_modified = EXCLUDED.last_modified,
                    etag = EXCLUDED.etag,
                    indexed_at = NOW();`

			batch.Queue(
				query,
				aws.ToString(obj.Key),
				obj.Size,
				aws.ToTime(obj.LastModified),
				aws.ToString(obj.ETag),
			)
			count++
		}

		// Execute the batch for the current page
		br := db.Pool.SendBatch(ctx, batch)
		if err := br.Close(); err != nil {
			return count, fmt.Errorf("db batch execution failed: %w", err)
		}

		slog.Debug("S3 batch page processed", "bucket", bucketName, "objects_in_page", len(page.Contents), "total_so_far", count)
	}

	return count, nil
}

// UpsertS3Objects handles multiple objects at once using pgx.Batch.
// This is used for processing SQS event batches or small-scale metadata updates.
// It returns the number of rows affected by the operation.
func (db *DB) UpsertS3Objects(ctx context.Context, objects []S3Object) (int64, error) {
	if len(objects) == 0 {
		return 0, nil
	}

	batch := &pgx.Batch{}
	for _, obj := range objects {
		query := `
            INSERT INTO s3_objects (file_key, file_size, last_modified, etag)
            VALUES ($1, $2, $3, $4)
            ON CONFLICT (file_key) DO UPDATE SET
                file_size = EXCLUDED.file_size,
                last_modified = EXCLUDED.last_modified,
                etag = EXCLUDED.etag,
                indexed_at = NOW();`
		
		batch.Queue(query, obj.FileKey, obj.FileSize, obj.LastModified, obj.ETag)
	}

	results := db.Pool.SendBatch(ctx, batch)
	defer results.Close()

	var rowsAffected int64
	for i := 0; i < len(objects); i++ {
		ct, err := results.Exec()
		if err != nil {
			slog.Error("Batch upsert item failure", "index", i, "error", err)
			return rowsAffected, fmt.Errorf("error executing batch item %d: %w", i, err)
		}
		rowsAffected += ct.RowsAffected()
	}

	return rowsAffected, nil
}

// DeleteS3Object removes an object entry from the database by its file key.
// Typically triggered by an s3:ObjectRemoved event via SQS.
func (db *DB) DeleteS3Object(ctx context.Context, fileKey string) error {
	_, err := db.Pool.Exec(ctx, "DELETE FROM s3_objects WHERE file_key = $1", fileKey)
	if err != nil {
		slog.Error("Failed to delete object from DB", "file_key", fileKey, "error", err)
		return err
	}
	slog.Debug("Object deleted from database", "file_key", fileKey)
	return nil
}