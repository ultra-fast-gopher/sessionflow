// Package beego adapts Beego v2's router filter chain to sessionflow.
package beego

import (
	"net/http"
	"time"

	"github.com/beego/beego/v2/server/web"
	"github.com/beego/beego/v2/server/web/context"
	"github.com/felixge/httpsnoop"
	"github.com/ultra-fast-gopher/sessionflow"
)

type statusTracker struct{ code int }

func trackStatus(w http.ResponseWriter, status *statusTracker) http.ResponseWriter {
	return httpsnoop.Wrap(w, httpsnoop.Hooks{WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
		return func(code int) {
			if status.code == 0 || (status.code >= 100 && status.code < 200 && status.code != http.StatusSwitchingProtocols && code >= 200) {
				status.code = code
			}
			next(code)
		}
	}})
}

// Config provides Beego-specific values that are available in its request context.
type Config struct {
	Session func(*context.Context) string
}

// FilterChain returns a Beego filter chain which wraps routing and controller
// execution. Register it on a ControllerRegister with InsertFilterChain("/*",
// sessionflowbeego.FilterChain(rec)). The matched route template is supplied by
// Beego's RouterPattern request data after dispatch.
func FilterChain(rec *sessionflow.Recorder, configs ...Config) web.FilterChain {
	var cfg Config
	for _, option := range configs {
		if option.Session != nil {
			cfg.Session = option.Session
		}
	}
	return func(next web.FilterFunc) web.FilterFunc {
		return func(ctx *context.Context) {
			req := ctx.Request
			span := rec.BeginRequest(req)
			if span == nil {
				next(ctx)
				return
			}
			start := time.Now()
			original := ctx.ResponseWriter.ResponseWriter
			var metrics httpsnoop.Metrics
			status := &statusTracker{}
			completed := false
			defer func() {
				ctx.ResponseWriter.ResponseWriter = original
				route, _ := ctx.Input.GetData("RouterPattern").(string)
				session := ""
				if completed {
					if cfg.Session != nil {
						session = cfg.Session(ctx)
					} else {
						session = rec.SessionID(req)
					}
					route = rec.RoutePattern(req, route)
				}
				metrics.Code = responseStatus(status.code, metrics.Code)
				span.End(sessionflow.Observation{Method: req.Method, Route: route,
					Session: session, Status: metrics.Code, Bytes: metrics.Written,
					BytesUnknown: metrics.Code == http.StatusSwitchingProtocols,
					Panicked:     !completed, Duration: time.Since(start)})
			}()
			metrics = httpsnoop.CaptureMetrics(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				ctx.ResponseWriter.ResponseWriter = w
				next(ctx)
			}), trackStatus(original, status), req)
			// Beego's context.Response rejects every WriteHeader after the first,
			// including a final status after 103 Early Hints. net/http sends its
			// implicit 200 when the body is written (or the handler returns).
			completed = true
		}
	}
}

func responseStatus(tracked, fallback int) int {
	switch {
	case tracked == http.StatusSwitchingProtocols:
		return tracked
	case tracked >= 200:
		return tracked
	case tracked >= 100 && tracked < 200:
		return http.StatusOK
	default:
		return fallback
	}
}
