package handlers

import "github.com/gin-gonic/gin"

func IndexHandler() gin.HandlerFunc {
    return func(c *gin.Context) {
        // Since this is a static landing page, cache it for an hour
        c.Header("Cache-Control", "public, max-age=3600")
        c.HTML(200, "index.html", gin.H{
            "title": "WikiSubmission Library API",
        })
    }
}