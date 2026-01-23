package handlers

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/wikisubmission/ws-lib/aws"
	"github.com/wikisubmission/ws-lib/db"
)

// SetCacheHeaders applies the appropriate Cache-Control headers based on file privacy.
func SetCacheHeaders(c *gin.Context, signer *aws.CFSigner, obj *db.S3Object, isSearch bool, shared bool) {
    // If it's a search result, we use a short cache (e.g., 5 mins) to keep results fresh
    // but still take load off the DB/Cloudflare.
    if isSearch {
        c.Header("Cache-Control", "public, max-age=300")
        return
    }

    if obj == nil {
        return
    }

    // For specific file access, check privacy
    if signer.IsPrivate(obj.FileKey) {
        // PRIVATE: Prevent all caching
        c.Header("Cache-Control", "no-store, no-cache, must-revalidate, proxy-revalidate")
        c.Header("Pragma", "no-cache")
        c.Header("Expires", "0")
    } else {
        // PUBLIC: Cache at Edge and Browser for 1 year
        c.Header("Cache-Control", "public, max-age=31536000, immutable")
        c.Header("X-Singleflight-Hit", strconv.FormatBool(shared))
        if obj.LastModified != "" { 
            c.Header("Last-Modified", obj.LastModified) 
        }
        if obj.ETag != "" { 
            c.Header("ETag", obj.ETag) 
        }
    }
}