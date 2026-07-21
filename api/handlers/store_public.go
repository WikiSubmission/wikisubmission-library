package handlers

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/gin-gonic/gin"
	"github.com/wikisubmission/ws-lib/aws"
)

const (
	// maxPublicStoreBytes bounds a single stored object. Offline content
	// bundles (SQLite files with FTS indexes) run larger than JSON exports,
	// so the cap is wider than the private store's.
	maxPublicStoreBytes = 100 << 20 // 100 MiB
	// defaultPublicCacheControl keeps accidental omissions cheap to fix: a
	// short TTL means a corrected re-upload propagates within a minute.
	defaultPublicCacheControl = "public, max-age=60"
)

// publicStorePrefixes are the only key prefixes the public store endpoint may
// write to. Each must be served publicly by CloudFront (no signed-URL
// behavior). offline/ holds the offline content bundles and their manifest;
// editorial/ holds images uploaded from the first-party content editor.
var publicStorePrefixes = []string{"offline/", "editorial/"}

type storePublicRequest struct {
	Key           string `json:"key"`
	ContentBase64 string `json:"content_base64"`
	ContentType   string `json:"content_type"`
	CacheControl  string `json:"cache_control"`
}

// StorePublicHandler stores a caller-provided object under an allowed public
// prefix and returns its stable public CloudFront URL. The endpoint itself is
// internal (behind the HMAC signature middleware, like /private/store);
// "public" refers to the stored object: there is no signed URL, no TTL, and
// no attachment disposition. Objects are world-readable at the returned URL
// and cached per the caller's Cache-Control.
func StorePublicHandler(s3Client *s3sdk.Client, signer *aws.CFSigner, bucket string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if bucket == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "storage not configured"})
			return
		}

		var req storePublicRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}

		key, err := validatePublicStoreKey(req.Key)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
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
		if len(payload) > maxPublicStoreBytes {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "content exceeds size limit"})
			return
		}

		contentType := req.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		cacheControl := req.CacheControl
		if cacheControl == "" {
			cacheControl = defaultPublicCacheControl
		}

		if err := aws.PutObject(c.Request.Context(), s3Client, bucket, key, payload, contentType, "", cacheControl); err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": "failed to store object"})
			return
		}

		url := signer.GetPublicURL(key)
		slog.Info("store-public: object stored", "key", key, "bytes", len(payload), "cache_control", cacheControl)
		c.JSON(http.StatusOK, gin.H{"key": key, "url": url})
	}
}

// validatePublicStoreKey normalizes the requested key and enforces the public
// prefix allowlist, so a caller can neither traverse paths nor write outside
// the prefixes CloudFront serves publicly.
func validatePublicStoreKey(raw string) (string, error) {
	key := strings.TrimPrefix(raw, "/")
	if strings.Contains(key, "..") {
		return "", fmt.Errorf("key must not contain '..'")
	}
	for _, prefix := range publicStorePrefixes {
		if strings.HasPrefix(key, prefix) && len(key) > len(prefix) {
			return key, nil
		}
	}
	return "", fmt.Errorf("key must be under a public prefix (%s)", strings.Join(publicStorePrefixes, ", "))
}
