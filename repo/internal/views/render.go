package views

import (
	"net/http"

	"github.com/a-h/templ"
	"github.com/gin-gonic/gin"
)

// Render renders a templ.Component into a Gin response.
func Render(c *gin.Context, status int, component templ.Component) {
	c.Status(status)
	c.Header("Content-Type", "text/html; charset=utf-8")
	if err := component.Render(c.Request.Context(), c.Writer); err != nil {
		c.String(http.StatusInternalServerError, "render error: %v", err)
	}
}
