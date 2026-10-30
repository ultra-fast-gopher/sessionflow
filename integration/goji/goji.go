// Package goji adapts Goji v2 route patterns to sessionflow.
package goji

import (
	"net/http"

	flow "github.com/ultra-fast-gopher/sessionflow"
	"goji.io/middleware"
)

// Middleware wraps Goji middleware and reads the matched Pattern after the
// handler. Patterns implementing fmt.Stringer provide their registered form.
func Middleware(rec *flow.Recorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return rec.WrapWithRoute(next, func(req *http.Request) string {
			pattern := middleware.Pattern(req.Context())
			if stringer, ok := pattern.(interface{ String() string }); ok {
				return stringer.String()
			}
			return ""
		})
	}
}
