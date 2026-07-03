package aws

import (
	"bytes"
	"context"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// PutObject writes an object to the bucket with the given content type,
// content disposition, and cache control (each skipped when empty). Used by
// the authenticated store endpoints so trusted internal services (e.g.
// ws-backend's data export and offline-bundle publisher) can stash
// service-generated artifacts without holding their own S3 or CloudFront
// credentials.
func PutObject(ctx context.Context, client *s3.Client, bucket, key string, body []byte, contentType, contentDisposition, cacheControl string) error {
	input := &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(body),
		ContentType: aws.String(contentType),
	}
	if contentDisposition != "" {
		input.ContentDisposition = aws.String(contentDisposition)
	}
	if cacheControl != "" {
		input.CacheControl = aws.String(cacheControl)
	}
	_, err := client.PutObject(ctx, input)
	if err != nil {
		slog.Error("S3 PutObject failed", slog.String("bucket", bucket), slog.String("key", key), slog.Any("error", err))
	}
	return err
}

// ListFilesInBucket returns a slice of all object keys in a bucket.
// Note: This only returns up to the first 1,000 keys. For full synchronization,
// use GetAllObjectsInBucket which handles pagination.
func ListFilesInBucket(client *s3.Client, bucketName string) ([]string, error) {
	output, err := client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		slog.Error("S3 ListObjectsV2 failed", slog.String("bucket", bucketName), slog.Any("error", err))
		return nil, err
	}

	var keys []string
	for _, object := range output.Contents {
		keys = append(keys, aws.ToString(object.Key))
	}
	return keys, nil
}

// CheckBucketAccess verifies S3 credentials and bucket accessibility.
// Uses GetBucketLocation (s3:GetBucketLocation) rather than ListObjectsV2 (s3:ListBucket),
// as the latter may be explicitly denied by restrictive identity-based policies.
// An authenticated GetBucketLocation response is sufficient to confirm valid credentials.
func CheckBucketAccess(ctx context.Context, client *s3.Client, bucketName string) error {
	_, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		slog.Error("S3 health check failed", slog.String("bucket", bucketName), slog.Any("error", err))
	}
	return err
}

// GetAllObjectsInBucket performs a full crawl of an S3 bucket using a paginator.
// It returns a slice of types.Object containing metadata for every file in the bucket.
func GetAllObjectsInBucket(client *s3.Client, bucketName string) ([]types.Object, error) {
	var allObjects []types.Object
	
	// Create a paginator to handle buckets with > 1000 objects
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})

	pageNum := 0
	for paginator.HasMorePages() {
		pageNum++
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			slog.Error("S3 Pagination failed", 
				slog.String("bucket", bucketName), 
				slog.Int("page", pageNum), 
				slog.Any("error", err),
			)
			return nil, err 
		}
		
		allObjects = append(allObjects, page.Contents...)
		
		slog.Debug("S3 page fetched", 
			slog.String("bucket", bucketName), 
			slog.Int("page", pageNum), 
			slog.Int("objects_in_page", len(page.Contents)),
		)
	}
	
	slog.Info("S3 bucket crawl complete", 
		slog.String("bucket", bucketName), 
		slog.Int("total_objects", len(allObjects)),
	)
	
	return allObjects, nil
}