package handlers

import (
	"encoding/base64"
	"log/slog"
	"net/http"
	"strings"
	"time"

	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/wikisubmission/ws-lib/aws"
)

const (
	// maxStoreTTL caps the signed-URL lifetime a caller can request. It matches
	// the longest sensible private-link window and the export lifecycle.
	maxStoreTTL = 30 * 24 * time.Hour
	// maxStoreBytes bounds a single stored object so a misbehaving caller cannot
	// stream an unbounded body into S3.
	maxStoreBytes = 25 << 20 // 25 MiB
)

type storeRequest struct {
	Key           string `json:"key"`
	ContentBase64 string `json:"content_base64"`
	ContentType   string `json:"content_type"`
	Disposition   string `json:"disposition"`
	TTLSeconds    int64  `json:"ttl_seconds"`
}

// StoreHandler stores a caller-provided object under the private/ prefix and
// returns a CloudFront signed URL for it. It centralises all S3 + CloudFront
// access in ws-lib: trusted internal services (authenticated by the signature
// middleware) such as ws-backend's data export call this instead of holding
// their own AWS credentials or signing keys.
func StoreHandler(s3Client *s3sdk.Client, signer *aws.CFSigner, bucket string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if bucket == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage not configured"})
			return
		}

		var req storeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}

		// Only private/ keys are allowed: the CloudFront private behavior signs
		// them, and it prevents a caller writing to public or arbitrary paths.
		key := strings.TrimPrefix(req.Key, "/")
		if !strings.HasPrefix(key, "private/") || strings.Contains(key, "..") {
			c.JSON(http.StatusBadRequest, gin.H{"error": "key must be under private/ and contain no '..'"})
			return
		}

		payload, err := base64.StdEncoding.DecodeString(req.ContentBase64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "content_base64 is not valid base64"})
			return
		}
		if len(payload) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "empty content"})
			return
		}
		if len(payload) > maxStoreBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "content exceeds size limit"})
			return
		}

		ttl := time.Duration(req.TTLSeconds) * time.Second
		if ttl <= 0 || ttl > maxStoreTTL {
			ttl = maxStoreTTL
		}
		contentType := req.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		if err := aws.PutObject(c.Request.Context(), s3Client, bucket, key, payload, contentType, req.Disposition); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to store object"})
			return
		}

		url, err := signer.GetSignedURL(key, ttl)
		if err != nil {
			slog.Error("store: signing failed", "key", key, "error", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to sign url"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"key": key, "url": url})
	}
}
