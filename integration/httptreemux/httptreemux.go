// Package httptreemux adapts dimfeld/httptreemux v5 route context to sessionflow.
package httptreemux

import (
	"net/http"

	treemux "github.com/dimfeld/httptreemux/v5"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

// Middleware wraps httptreemux middleware. Register it through UsingContext().
// ContextRoute returns the normalized registered pattern after dispatch.
func Middleware(rec *flow.Recorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return rec.WrapWithRoute(next, func(req *http.Request) string {
			return treemux.ContextRoute(req.Context())
		})
	}
}
