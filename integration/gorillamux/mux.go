// Package gorillamux adapts Gorilla Mux route templates to sessionflow.
package gorillamux

import (
	"net/http"

	"github.com/gorilla/mux"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

// Middleware wraps a mux middleware chain. Register with Router.Use; the
// matched route is inspected after downstream handlers run. Gorilla Mux only
// invokes registered middleware for matched routes; its 404/405 handlers are
// outside this middleware chain and are not observed.
func Middleware(rec *flow.Recorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return rec.WrapWithRoute(next, func(req *http.Request) string {
			if route := mux.CurrentRoute(req); route != nil {
				template, _ := route.GetPathTemplate()
				return template
			}
			return ""
		})
	}
}

// Install adds route-aware request middleware and instruments Gorilla Mux's
// not-found and method-not-allowed handlers. Set custom fallback handlers first,
// then call Install once before registering other middleware so sessionflow is
// outermost in the router middleware chain. Existing fallback behavior is kept.
func Install(router *mux.Router, rec *flow.Recorder) {
	router.Use(Middleware(rec))
	notFound := router.NotFoundHandler
	if notFound == nil {
		notFound = http.NotFoundHandler()
	}
	methodNotAllowed := router.MethodNotAllowedHandler
	if methodNotAllowed == nil {
		methodNotAllowed = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusMethodNotAllowed)
		})
	}
	router.NotFoundHandler = rec.WrapWithRoute(notFound, func(*http.Request) string { return "<unmatched>" })
	router.MethodNotAllowedHandler = rec.WrapWithRoute(methodNotAllowed, func(*http.Request) string { return "<unmatched>" })
}
