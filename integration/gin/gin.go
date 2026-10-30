// Package gin adapts Gin requests to sessionflow captures. Register it with
// router.Use(gin.Middleware(recorder)) after the router's recovery middleware.
package gin

import (
	"github.com/gin-gonic/gin"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

type Config struct {
	// Session returns an authenticated user or session identifier after handlers run.
	Session func(*gin.Context) string
	// Route can replace Gin's matched route template.
	Route func(*gin.Context, string) string
}

// Middleware records Gin's FullPath template and final response status and size.
// Gin 404/405 requests have no FullPath and are recorded as <unmatched>.
func Middleware(rec *flow.Recorder, configs ...Config) gin.HandlerFunc {
	cfg := Config{}
	if len(configs) > 0 {
		cfg = configs[0]
	}
	return func(c *gin.Context) {
		span := rec.BeginRequest(c.Request)
		if span == nil {
			c.Next()
			return
		}
		completed := false
		defer func() {
			if !completed {
				finish(rec, cfg, c, span, true)
			}
		}()
		c.Next()
		completed = true
		finish(rec, cfg, c, span, false)
	}
}

func finish(rec *flow.Recorder, cfg Config, c *gin.Context, span *flow.Span, panicked bool) {
	if panicked {
		bytes := c.Writer.Size()
		if bytes < 0 {
			bytes = 0
		}
		span.End(flow.Observation{Method: c.Request.Method, Route: "<unmatched>", Status: c.Writer.Status(), Bytes: int64(bytes), Panicked: true})
		return
	}
	route := c.FullPath()
	if cfg.Route != nil {
		route = cfg.Route(c, route)
	}
	session := ""
	if cfg.Session != nil {
		session = cfg.Session(c)
	} else {
		session = rec.SessionID(c.Request)
	}
	bytes := c.Writer.Size()
	if bytes < 0 {
		bytes = 0
	}
	span.End(flow.Observation{Method: c.Request.Method, Route: rec.RoutePattern(c.Request, route), Session: session,
		Status: c.Writer.Status(), Bytes: int64(bytes), Panicked: panicked})
}
