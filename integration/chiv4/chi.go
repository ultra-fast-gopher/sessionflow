// Package chiv4 adapts chi v4 route patterns to sessionflow.
package chiv4

import (
	"net/http"

	chi "github.com/go-chi/chi"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

// Middleware wraps a chi v4 middleware chain. Add it with Router.Use; route
// context is read after downstream handlers so nested routes are complete.
func Middleware(rec *flow.Recorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return rec.WrapWithRoute(next, func(req *http.Request) string {
			if route := chi.RouteContext(req.Context()); route != nil {
				return route.RoutePattern()
			}
			return ""
		})
	}
}
