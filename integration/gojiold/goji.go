// Package gojiold adapts the historical github.com/zenazn/goji router.
package gojiold

import (
	"net/http"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/ultra-fast-gopher/sessionflow"
	"github.com/zenazn/goji/web"
)

// Config provides values available in Goji's per-request context.
type Config struct {
	Session func(web.C, *http.Request) string
}

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

// Middleware wraps a Goji handler registered for routePattern. Goji v0.9 passes
// URL parameters in its request context but does not expose the matched pattern,
// so pass the same pattern used with web.Get/web.Post.
func Middleware(rec *sessionflow.Recorder, routePattern string, next http.Handler) web.Handler {
	return Handler(rec, routePattern, httpHandler{next})
}

type httpHandler struct{ http.Handler }

func (h httpHandler) ServeHTTPC(_ web.C, w http.ResponseWriter, r *http.Request) {
	h.ServeHTTP(w, r)
}

// Handler wraps a native Goji handler and preserves web.C, including URLParams.
// Use this instead of Middleware when the downstream handler accepts Goji's
// context or when Config.Session needs that context.
func Handler(rec *sessionflow.Recorder, routePattern string, next web.Handler, configs ...Config) web.Handler {
	var cfg Config
	for _, option := range configs {
		if option.Session != nil {
			cfg.Session = option.Session
		}
	}
	return web.HandlerFunc(func(c web.C, w http.ResponseWriter, req *http.Request) {
		span := rec.BeginRequest(req)
		if span == nil {
			next.ServeHTTPC(c, w, req)
			return
		}
		start := time.Now()
		completed := false
		metrics := httpsnoop.Metrics{Code: http.StatusOK}
		status := &statusTracker{}
		defer func() {
			session := ""
			resolvedRoute := routePattern
			if completed {
				if cfg.Session != nil {
					session = cfg.Session(c, req)
				} else {
					session = rec.SessionID(req)
				}
				resolvedRoute = rec.RoutePattern(req, routePattern)
			}
			statusCode := responseStatus(status.code, metrics.Code)
			span.End(sessionflow.Observation{Method: req.Method, Route: resolvedRoute,
				Session: session, Status: statusCode, Bytes: metrics.Written,
				BytesUnknown: statusCode == http.StatusSwitchingProtocols,
				Panicked:     !completed, Duration: time.Since(start)})
		}()
		metrics = httpsnoop.CaptureMetrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTPC(c, w, r)
		}), trackStatus(w, status), req)
		completed = true
	})
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
