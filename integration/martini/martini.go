// Package martini adapts the Martini router to sessionflow.
package martini

import (
	"net/http"
	"time"

	"github.com/go-martini/martini"
	"github.com/ultra-fast-gopher/sessionflow"
)

// Config provides values available in Martini's request context.
type Config struct {
	Session func(martini.Context, *http.Request) string
}

// Middleware records a matched Martini route. Register it as a handler on each
// route (for example, m.Get("/users/:id", Middleware(rec), handler)); Martini
// injects its native Route only inside the matched route handler chain. A
// generic m.Use middleware cannot obtain this route template.
func Middleware(rec *sessionflow.Recorder, configs ...Config) martini.Handler {
	var cfg Config
	for _, option := range configs {
		if option.Session != nil {
			cfg.Session = option.Session
		}
	}
	return func(ctx martini.Context, w http.ResponseWriter, req *http.Request, route martini.Route) {
		span := rec.BeginRequest(req)
		if span == nil {
			ctx.Next()
			return
		}
		start := time.Now()
		rw, _ := w.(martini.ResponseWriter)
		completed := false
		defer func() {
			status, bytes := http.StatusOK, int64(0)
			if rw != nil {
				status = rw.Status()
				bytes = int64(rw.Size())
			}
			if status == 0 {
				status = http.StatusOK
			}
			if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
				status = http.StatusOK
			}
			session := ""
			routePattern := route.Pattern()
			if completed {
				if cfg.Session != nil {
					session = cfg.Session(ctx, req)
				} else {
					session = rec.SessionID(req)
				}
				routePattern = rec.RoutePattern(req, routePattern)
			}
			span.End(sessionflow.Observation{Method: req.Method, Route: routePattern,
				Session: session, Status: status, Bytes: bytes, BytesUnknown: status == http.StatusSwitchingProtocols,
				Panicked: !completed, Duration: time.Since(start)})
		}()
		ctx.Next()
		completed = true
	}
}
