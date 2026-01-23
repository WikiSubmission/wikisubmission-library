package aws

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// InitAWSConfig loads the default AWS configuration from the environment
// and initializes global S3 and SQS clients.
func InitAWSConfig() (*s3.Client, *sqs.Client, error) {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		slog.Warn("AWS_REGION not set, falling back to SDK default")
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
	if err != nil {
		slog.Error("Unable to load AWS SDK config", slog.Any("error", err))
		return nil, nil, fmt.Errorf("unable to load SDK config: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg)
	sqsClient := sqs.NewFromConfig(cfg)

	slog.Info("AWS Clients initialized", "region", region)
	return s3Client, sqsClient, nil
}


// GetURL determines if a file needs a signed URL based on its path.
// It matches CloudFront behaviors: 
// 1. If the path starts with /private/, it generates a Signed URL.
// 2. Otherwise, it returns a standard Public URL.
func (s *CFSigner) GetURL(fileKey string, duration time.Duration) (string, error) {
	// Standardize the key by ensuring it starts with a slash for the prefix check
	// This ensures consistency between S3 keys and CloudFront URL paths
	pathKey := fileKey
	if !strings.HasPrefix(pathKey, "/") {
		pathKey = "/" + pathKey
	}

	// Logic for CloudFront private behavior
	if strings.HasPrefix(pathKey, "/private/") {
		slog.Debug("Generating signed URL for private content", "key", fileKey)
		return s.GetSignedURL(fileKey, duration)
	}

	// Default behavior for public content
	return s.GetPublicURL(fileKey), nil
}