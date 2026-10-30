// Package restful adapts go-restful v3 container filters to sessionflow.
package restful

import (
	"net/http"
	"time"

	rf "github.com/emicklei/go-restful/v3"
	"github.com/felixge/httpsnoop"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

// Filter returns a go-restful container filter. It records matched routes using
// Request.SelectedRoutePath after dispatch. Unmatched/error routes are left to
// go-restful's service error handler and are not observed by this filter.
func Filter(rec *flow.Recorder) rf.FilterFunction {
	return func(req *rf.Request, resp *rf.Response, chain *rf.FilterChain) {
		template := req.SelectedRoutePath()
		if template == "" {
			chain.ProcessFilter(req, resp)
			return
		}
		span := rec.BeginRequest(req.Request)
		if span == nil {
			chain.ProcessFilter(req, resp)
			return
		}
		start := time.Now()
		metrics := httpsnoop.Metrics{Code: http.StatusOK}
		completed := false
		original := resp.ResponseWriter
		defer func() {
			resp.ResponseWriter = original
			if !completed {
				metrics.Duration = time.Since(start)
			}
			route := template
			session := ""
			if completed {
				route = rec.RoutePattern(req.Request, template)
				session = rec.SessionID(req.Request)
			} else if cookie, cookieErr := req.Request.Cookie(rec.CookieName()); cookieErr == nil {
				session = cookie.Value
			}
			span.End(flow.Observation{Method: req.Request.Method, Route: route, Session: session,
				Status: metrics.Code, Bytes: metrics.Written, Panicked: !completed, Duration: metrics.Duration})
		}()
		metrics = httpsnoop.CaptureMetrics(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			resp.ResponseWriter = w
			chain.ProcessFilter(req, resp)
		}), original, req.Request)
		completed = true
	}
}
