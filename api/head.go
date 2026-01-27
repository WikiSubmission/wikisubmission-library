package main

import "github.com/gin-gonic/gin"

type headWriter struct {
	gin.ResponseWriter
}

func (w *headWriter) Write(data []byte) (int, error) {
	return len(data), nil // Pretend to write, but do nothing
}

func (w *headWriter) WriteString(s string) (int, error) {
	return len(s), nil // Pretend to write, but do nothing
}

// SupportHEAD allows HEAD requests to be handled by GET routes without sending a body.
func SupportHEAD(c *gin.Context) {
	if c.Request.Method == "HEAD" {
		c.Request.Method = "GET"
		c.Writer = &headWriter{c.Writer}
	}
	c.Next()
}