// Package macaron adapts Macaron v1 route handlers to sessionflow.
package macaron

import (
	"net/http"
	"time"

	"github.com/ultra-fast-gopher/sessionflow"
	"gopkg.in/macaron.v1"
)

// Config provides values available in Macaron's request context.
type Config struct {
	Session func(*macaron.Context) string
}

// Middleware records requests for a specific Macaron route. Add it to that
// route's handler list, for example m.Get("/users/:id", Middleware(rec,
// "/users/:id"), handler). Macaron does not expose the matched route template
// through Context, so callers provide the registered pattern explicitly.
func Middleware(rec *sessionflow.Recorder, routePattern string, configs ...Config) macaron.Handler {
	var cfg Config
	for _, option := range configs {
		if option.Session != nil {
			cfg.Session = option.Session
		}
	}
	return func(ctx *macaron.Context) {
		req := ctx.Req.Request
		span := rec.BeginRequest(req)
		if span == nil {
			ctx.Next()
			return
		}
		start := time.Now()
		completed := false
		defer func() {
			status := ctx.Resp.Status()
			if status == 0 {
				status = http.StatusOK
			}
			if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
				status = http.StatusOK
			}
			session := ""
			resolvedRoute := routePattern
			if completed {
				if cfg.Session != nil {
					session = cfg.Session(ctx)
				} else {
					session = rec.SessionID(req)
				}
				resolvedRoute = rec.RoutePattern(req, routePattern)
			}
			span.End(sessionflow.Observation{Method: req.Method, Route: resolvedRoute,
				Session: session, Status: status, Bytes: int64(ctx.Resp.Size()), BytesUnknown: status == http.StatusSwitchingProtocols,
				Panicked: !completed, Duration: time.Since(start)})
		}()
		ctx.Next()
		completed = true
	}
}
