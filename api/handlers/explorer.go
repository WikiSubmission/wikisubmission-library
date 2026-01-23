package handlers

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wikisubmission/ws-lib/db"
)

func ExplorerHandler(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		query := c.DefaultQuery("q", "")
		limitStr := c.DefaultQuery("limit", "50")
		isPartial := c.Query("partial") == "true"

		limit, err := strconv.Atoi(limitStr)
			if err != nil {
				limit = 50
			}
		
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()

		c.Header("Cache-Control", "public, max-age=60")

		data, err := database.GetExplorerData(ctx, query, limit)
    
		if err != nil {
			slog.Error("Explorer error", "error", err)
			c.String(500, "Internal Server Error")
			return
		}

    	if isPartial {
			// Render ONLY the file cards/directories fragment
			c.HTML(200, "explorer_fragments.html", data)
			return
		}

		c.HTML(200, "explorer.html", data)
	}
}