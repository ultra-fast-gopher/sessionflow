// Package gozero adapts go-zero REST middleware and route handlers to sessionflow.
package gozero

import (
	"net/http"

	flow "github.com/ultra-fast-gopher/sessionflow"
	"github.com/zeromicro/go-zero/rest"
)

// Middleware adapts sessionflow to Server.Use. go-zero does not expose the
// matched route template to generic middleware, so use Handler when a concrete
// template is needed on matched requests.
func Middleware(rec *flow.Recorder) rest.Middleware {
	return rest.ToMiddleware(func(next http.Handler) http.Handler {
		return rec.WrapWithRoute(next, nil)
	})
}

// Handler wraps a route-specific handler with its registered template. Register
// this with go-zero's router using the same template, never the request URL.
func Handler(rec *flow.Recorder, template string, next http.Handler) http.Handler {
	return rec.WrapWithRoute(next, func(*http.Request) string { return template })
}
