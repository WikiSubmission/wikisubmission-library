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

		// Singleflight for search queries
        // Key is query + limit to ensure unique results are cached correctly
        cacheKey := "search:" + query + ":" + strconv.Itoa(limit)
        val, err, _ := requestGroup.Do(cacheKey, func() (interface{}, error) {
            return database.SearchObjects(c.Request.Context(), query, limit)
        })

        if err != nil {
            slog.Error("Search failed", slog.String("query", query), slog.Any("error", err))
            c.JSON(http.StatusInternalServerError, gin.H{"error": "Search failed"})
            return
        }

        results := val.([]db.S3Object)
        if results == nil { results = []db.S3Object{} }

        // Use the new Cache Helper
		SetCacheHeaders(c, signer, &db.S3Object{}, false, false)

        for i := range results {
            url, err := signer.GetURL(results[i].FileKey, 1*time.Hour)
            if err != nil { continue }
            results[i].DownloadURL = url
        }

        c.JSON(http.StatusOK, results)
    }
}