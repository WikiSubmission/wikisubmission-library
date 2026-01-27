package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/wikisubmission/ws-lib/aws"
	"github.com/wikisubmission/ws-lib/db"
)

func FileHandler (database *db.DB, signer *aws.CFSigner ) gin.HandlerFunc {
return func(c *gin.Context) {
	fileKey := c.Param("filepath")	

	if len(fileKey) > 0 && fileKey[0] == '/' {
		fileKey = fileKey[1:]
	}

	if fileKey == "" {
		c.Redirect(http.StatusFound, "/explorer")
		return 
	}
	result, err := database.SearchObjects(c.Request.Context(), fileKey, 1)
	if err != nil {
		c.Redirect(http.StatusFound, "/explorer")
		return 
	}
	if len(result) == 0 {
		c.Redirect(http.StatusFound, "/explorer")
		return 
	}
	url, err := signer.GetURL(result[0].FileKey, 1*time.Hour)

	if (err != nil) {
		slog.Error("Signer error", "key", fileKey, "error", err)
		c.Redirect(http.StatusFound, "/explorer")
	}

	c.Redirect(http.StatusSeeOther, url)
}
}