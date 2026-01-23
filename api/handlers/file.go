package handlers

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wikisubmission/ws-lib/aws"
	"github.com/wikisubmission/ws-lib/db"
	"golang.org/x/sync/singleflight"
)

var requestGroup singleflight.Group

// FileHandler provides direct, high-speed access to S3 objects via their file path.
// It is designed to work with Gin's wildcard routing (e.g., r.GET("/file/*filepath", ...)).
//
// Logic Flow:
// 1. Extracts the raw path from the URL and sanitizes leading slashes.
// 2. Performs an O(log n) exact-match lookup in the database using a B-tree index.
// 3. If the file exists: Generates a temporary signed CloudFront/S3 URL and redirects the user (303).
// 4. If the file is missing: Gracefully redirects the user to the /explorer search page (302)
//    using the path as the initial search query.
//
// Parameters:
// - database: Pointer to the DB instance for B-tree key lookups.
// - signer: The AWS CloudFront/S3 signer for generating secure, temporary access links.
func FileHandler(database *db.DB, signer *aws.CFSigner) gin.HandlerFunc {
	return func(c *gin.Context) {		
		// Capture everything after "/file/" in the URL.
		fileKey := c.Param("filepath")

		// Standardize the key by removing leading slashes to match S3 storage patterns.
		if len(fileKey) > 0 && fileKey[0] == '/' {
			fileKey = fileKey[1:]
		}

		// If no path is provided, send the user to the general explorer.
		if fileKey == "" {
			c.Redirect(http.StatusFound, "/explorer")
			return
		}

		result, err, shared := requestGroup.Do(fileKey, func() (interface{}, error) {
			return database.GetObjectByKey(c.Request.Context(), fileKey)
		})

		// Cast the result back to our object type
		obj, _ := result.(*db.S3Object)

		// Handle missing files (Redirect to explorer for fuzzy search)
		if err != nil || obj == nil {
			slog.Info("Exact match not found, redirecting to explorer", "key", fileKey)
			c.Redirect(http.StatusFound, "/explorer?q="+url.QueryEscape(fileKey))
			return
		}

		// Generate the URL (Signer handles IsPrivate logic internally)
		finalURL, err := signer.GetURL(obj.FileKey, 1*time.Hour)
		if err != nil {
			slog.Error("Signer error", "key", fileKey, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to generate access link"})
			return
		}

		SetCacheHeaders(c, signer, obj, false, shared)

		// Redirect (303 See Other) to the signed storage URL.
		c.Redirect(http.StatusSeeOther, finalURL)
	}
}