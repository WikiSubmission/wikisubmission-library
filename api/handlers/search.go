package handlers

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wikisubmission/ws-lib/aws"
	"github.com/wikisubmission/ws-lib/db"
)

// SearchHandler returns a gin.HandlerFunc that performs fuzzy searches on S3 object metadata.
// It accepts a 'q' query parameter for the search term and an optional 'limit' parameter.
// It automatically handles URL generation (public or signed) for each search result.
func SearchHandler(database *db.DB, signer *aws.CFSigner) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.Query("q")
		if query == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter 'q' is required"})
			return
		}

		// Parse limit, default to 10 if missing or invalid
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))

		// Execute fuzzy search using the database GIN trigram index
		results, err := database.SearchObjects(c.Request.Context(), query, limit)
		if err != nil {
			slog.Error("Search operation failed", 
				slog.String("query", query), 
				slog.Any("error", err),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Search failed"})
			return
		}

		// Ensure we return an empty JSON array [] instead of null
		if results == nil {
			results = []db.S3Object{}
		}

		// Generate URLs for all results. 
		// Public URLs are returned as-is; Private keys get a 1-hour signed URL.
		for i := range results {
			url, err := signer.GetURL(results[i].FileKey, 1*time.Hour)
			if err != nil {
				slog.Warn("Failed to sign URL for search result", 
					slog.String("file_key", results[i].FileKey), 
					slog.Any("error", err),
				)
				continue 
			}
			results[i].DownloadURL = url
		}

		c.JSON(http.StatusOK, results)
	}
}