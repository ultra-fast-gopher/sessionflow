// Package iris adapts Iris v12 request middleware to sessionflow.
package iris

import (
	"net/http"
	"time"

	"github.com/kataras/iris/v12"
	"github.com/ultra-fast-gopher/sessionflow"
)

// Config provides Iris-specific data that is only available in an Iris context.
type Config struct {
	Session func(iris.Context) string
}

// Middleware records requests after Iris has resolved the current route. Install
// it with app.UseRouter so it also observes unmatched requests. Session IDs are
// read through Recorder.SessionID, including any configured session callback.
func Middleware(rec *sessionflow.Recorder, configs ...Config) iris.Handler {
	var cfg Config
	for _, option := range configs {
		if option.Session != nil {
			cfg.Session = option.Session
		}
	}
	return func(ctx iris.Context) {
		req := ctx.Request()
		span := rec.BeginRequest(req)
		if span == nil {
			ctx.Next()
			return
		}
		start := time.Now()
		completed := false
		defer func() {
			route := ""
			if current := ctx.GetCurrentRoute(); current != nil {
				route = current.Path()
			}
			writer := ctx.ResponseWriter()
			bytes := writer.Written()
			if bytes < 0 {
				bytes = 0
			}
			session := ""
			if completed {
				if cfg.Session != nil {
					session = cfg.Session(ctx)
				} else {
					session = rec.SessionID(req)
				}
				route = rec.RoutePattern(req, route)
			}
			status := writer.StatusCode()
			if status >= 100 && status < 200 && status != http.StatusSwitchingProtocols {
				status = http.StatusOK
			}
			span.End(sessionflow.Observation{Method: req.Method, Route: route,
				Session: session, Status: status, Bytes: int64(bytes), BytesUnknown: status == http.StatusSwitchingProtocols,
				Panicked: !completed, Duration: time.Since(start)})
		}()
		ctx.Next()
		completed = true
	}
}
