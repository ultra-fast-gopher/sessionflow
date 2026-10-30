// Package bunrouter adapts uptrace/bunrouter to sessionflow.
package bunrouter

import (
	"net/http"
	"time"

	"github.com/felixge/httpsnoop"
	flow "github.com/ultra-fast-gopher/sessionflow"
	bun "github.com/uptrace/bunrouter"
)

// Middleware returns bunrouter middleware. Bunrouter provides the matched
// template directly through Request.Route, including routes in nested groups.
// Use MiddlewareWithErrorHandler when a returned error is rendered into a
// response and its final status and byte count should be measured here.
func Middleware(rec *flow.Recorder) bun.MiddlewareFunc {
	return MiddlewareWithErrorHandler(rec, nil)
}

// ErrorHandler can render an error returned by a bunrouter handler. It returns
// the error that should continue up the bunrouter handler chain; returning nil
// consumes it. Use this when response metrics need to include a custom error
// response, because the handler middleware alone cannot observe later writes.
type ErrorHandler func(http.ResponseWriter, bun.Request, error) error

// MiddlewareWithErrorHandler wraps bunrouter middleware and optionally renders
// returned errors before measuring the response. The callback's returned error
// is propagated unchanged.
func MiddlewareWithErrorHandler(rec *flow.Recorder, errorHandler ErrorHandler) bun.MiddlewareFunc {
	return func(next bun.HandlerFunc) bun.HandlerFunc {
		return func(w http.ResponseWriter, req bun.Request) (err error) {
			span := rec.BeginRequest(req.Request)
			if span == nil {
				err = next(w, req)
				if err != nil && errorHandler != nil {
					err = errorHandler(w, req, err)
				}
				return err
			}
			start := time.Now()
			metrics := httpsnoop.Metrics{Code: http.StatusOK}
			completed := false
			defer func() {
				if !completed {
					metrics.Duration = time.Since(start)
				}
				route := req.Route()
				session := ""
				if completed {
					route = rec.RoutePattern(req.Request, route)
					session = rec.SessionID(req.Request)
				} else if cookie, cookieErr := req.Cookie(rec.CookieName()); cookieErr == nil {
					session = cookie.Value
				}
				span.End(flow.Observation{Method: req.Method, Route: route, Session: session,
					Status: metrics.Code, Bytes: metrics.Written, Panicked: !completed, Duration: metrics.Duration})
			}()
			metrics = httpsnoop.CaptureMetrics(http.HandlerFunc(func(captured http.ResponseWriter, _ *http.Request) {
				err = next(captured, req)
				if err != nil && errorHandler != nil {
					err = errorHandler(captured, req, err)
				}
			}), w, req.Request)
			completed = true
			return err
		}
	}
}
