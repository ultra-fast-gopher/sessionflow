// Package httprouter adapts julienschmidt/httprouter to sessionflow.
package httprouter

import (
	"context"
	"net/http"

	router "github.com/julienschmidt/httprouter"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

// Middleware adapts ordinary net/http handlers and uses Request.Pattern when
// supplied by an outer router. httprouter v1.3.0 does not expose the matched
// template to generic middleware, so register matched routes with Handler.
func Middleware(rec *flow.Recorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return rec.WrapWithRoute(next, nil) }
}

// Handler returns an httprouter handler that records the supplied registered
// route template. Use it with Router.Handle; never substitute the raw URL path.
func Handler(rec *flow.Recorder, route string, next http.Handler) router.Handle {
	return Handle(rec, route, func(w http.ResponseWriter, req *http.Request, _ router.Params) {
		next.ServeHTTP(w, req)
	})
}

// Handle wraps a native httprouter.Handle and preserves its Params argument.
// Use it with Router.Handle(method, template, Handle(rec, template, handler)).
func Handle(rec *flow.Recorder, route string, next router.Handle) router.Handle {
	wrapped := rec.WrapWithRoute(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		next(w, req, router.ParamsFromContext(req.Context()))
	}), func(*http.Request) string { return route })
	return func(w http.ResponseWriter, req *http.Request, params router.Params) {
		ctx := context.WithValue(req.Context(), router.ParamsKey, params)
		wrapped.ServeHTTP(w, req.WithContext(ctx))
	}
}
