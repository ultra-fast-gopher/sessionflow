// Package revel adapts Revel filters to sessionflow.
package revel

import (
	"io"
	"net/http"
	"time"

	"github.com/felixge/httpsnoop"
	"github.com/revel/revel"
	"github.com/ultra-fast-gopher/sessionflow"
)

// Config provides values available in a Revel controller after its filter chain.
type Config struct {
	Session func(*revel.Controller) string
}

// Filter creates a Revel filter which wraps the remaining filter chain. Route
// may resolve the controller to its registered template; when omitted, the
// adapter uses RouteTemplate when that action has exactly one registered route.
// To install, prepend this filter to revel.Filters so it surrounds routing and
// execution.
func Filter(rec *sessionflow.Recorder, route func(*revel.Controller) string, configs ...Config) revel.Filter {
	var cfg Config
	for _, option := range configs {
		if option.Session != nil {
			cfg.Session = option.Session
		}
	}
	return func(c *revel.Controller, chain []revel.Filter) {
		var req *http.Request
		if c.Request != nil && c.Request.In != nil {
			req, _ = c.Request.In.GetRaw().(*http.Request)
		}
		span := rec.BeginRequest(req)
		if span == nil {
			run(c, chain)
			return
		}
		start := time.Now()
		completed := false
		defer func() {
			routePattern := ""
			if !completed {
				routePattern = RouteTemplate(c)
			} else if route != nil {
				routePattern = route(c)
			} else {
				routePattern = RouteTemplate(c)
			}
			method := ""
			if req != nil {
				method = req.Method
			}
			session := ""
			if completed {
				if cfg.Session != nil {
					session = cfg.Session(c)
				} else {
					session = rec.SessionID(req)
				}
			}
			if completed {
				routePattern = rec.RoutePattern(req, routePattern)
			}
			obs := sessionflow.Observation{Method: method, Route: routePattern, Session: session,
				Status: http.StatusOK, Panicked: !completed, Duration: time.Since(start), BytesUnknown: true}
			if completed && c.Result != nil {
				c.Result = resultObserver{result: c.Result, span: span, observation: obs, started: start}
				return
			}
			span.End(obs)
		}()
		run(c, chain)
		completed = true
	}
}

type resultObserver struct {
	result      revel.Result
	span        *sessionflow.Span
	observation sessionflow.Observation
	started     time.Time
}

func (r resultObserver) Apply(req *revel.Request, resp *revel.Response) {
	var original io.Writer
	var metrics httpsnoop.Metrics
	var counter *countingWriter
	metricsAvailable := false
	streamResult := false
	if _, ok := r.result.(*revel.BinaryResult); ok {
		streamResult = true
	}
	completed := false
	defer func() {
		if resp != nil {
			if original != nil {
				resp.SetWriter(original)
			}
			if resp.Status != 0 {
				r.observation.Status = resp.Status
			}
		}
		if counter != nil {
			r.observation.Bytes = counter.bytes
			r.observation.BytesUnknown = false
		} else if metricsAvailable {
			r.observation.Bytes = metrics.Written
			r.observation.BytesUnknown = false
		}
		if resp != nil && resp.Status >= 100 && resp.Status < 200 && resp.Status != http.StatusSwitchingProtocols {
			r.observation.Status = http.StatusOK
		}
		if resp != nil && resp.Status == http.StatusSwitchingProtocols {
			r.observation.BytesUnknown = true
		}
		r.observation.Duration = time.Since(r.started)
		r.observation.Panicked = !completed
		r.span.End(r.observation)
	}()
	if resp != nil {
		original = resp.GetWriter()
		if original != nil && !streamResult {
			if writer, ok := original.(http.ResponseWriter); ok {
				metrics = httpsnoop.CaptureMetricsFn(writer, func(w http.ResponseWriter) {
					resp.SetWriter(w)
					r.result.Apply(req, resp)
				})
				metricsAvailable = true
			} else {
				counter = &countingWriter{writer: original}
				resp.SetWriter(counter)
				r.result.Apply(req, resp)
			}
		} else {
			r.result.Apply(req, resp)
		}
	} else {
		r.result.Apply(req, resp)
	}
	completed = true
}

type countingWriter struct {
	writer io.Writer
	bytes  int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.bytes += int64(n)
	return n, err
}

// RouteTemplate returns the registered path for a controller action only when
// Revel has exactly one route for that action. Aliases need an explicit Route
// callback passed to Filter because Controller does not retain the selected
// source route after RouterFilter.
func RouteTemplate(c *revel.Controller) string {
	if c == nil || revel.MainRouter == nil {
		return ""
	}
	var path string
	for _, candidate := range revel.MainRouter.Routes {
		if candidate.ControllerName != c.Name || candidate.MethodName != c.MethodName {
			continue
		}
		if candidate.Method != "*" && c.Request != nil && candidate.Method != c.Request.Method {
			continue
		}
		if path != "" {
			return ""
		}
		path = candidate.Path
	}
	return path
}

func run(c *revel.Controller, chain []revel.Filter) {
	if len(chain) > 0 {
		chain[0](c, chain[1:])
	}
}
