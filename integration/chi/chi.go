// Package chi adapts chi v5 route patterns to sessionflow.
package chi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

// Middleware wraps a chi middleware chain. Add it with Router.Use; chi's route
// context is read after the downstream handler, when nested routes are known.
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
