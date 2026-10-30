// Package echov5 adapts Echo v5 requests to sessionflow captures. Set the
// application's HTTPErrorHandler first, then call Install(e, recorder) before
// serving so returned errors are captured after the real error response. The
// response writer wrapper preserves its optional HTTP interfaces.
package echov5

import (
	"io"
	"net/http"
	"sync/atomic"

	"github.com/felixge/httpsnoop"
	"github.com/labstack/echo/v5"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

const spanKey = "sessionflow.echo.v5.span"

type Config struct {
	Session func(*echo.Context) string
	Route   func(*echo.Context, string) string
}
type pending struct {
	span     *flow.Span
	rec      *flow.Recorder
	cfg      Config
	original http.ResponseWriter
	status   atomic.Int64
	bytes    atomic.Int64
	finished atomic.Bool
}

// Middleware records the matched route. InstallErrorHandler captures errors after
// Echo's configured HTTPErrorHandler has written the response.
func Middleware(rec *flow.Recorder, configs ...Config) echo.MiddlewareFunc {
	cfg := Config{}
	if len(configs) > 0 {
		cfg = configs[0]
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			span := rec.BeginRequest(c.Request())
			if span == nil {
				return next(c)
			}
			p := &pending{span: span, rec: rec, cfg: cfg}
			c.Set(spanKey, p)
			original := c.Response()
			p.original = original
			c.SetResponse(wrapResponseWriter(p, original))
			completed := false
			defer func() {
				if !completed {
					finish(c, p, true)
				}
			}()
			err := next(c)
			completed = true
			if err != nil {
				return err
			}
			finish(c, p, false)
			return nil
		}
	}
}

func wrapResponseWriter(p *pending, w http.ResponseWriter) http.ResponseWriter {
	return httpsnoop.Wrap(w, httpsnoop.Hooks{
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(code int) {
				if code < 100 || code >= 200 || code == http.StatusSwitchingProtocols {
					p.status.CompareAndSwap(0, int64(code))
				}
				next(code)
			}
		},
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(b []byte) (int, error) {
				p.status.CompareAndSwap(0, http.StatusOK)
				n, err := next(b)
				p.bytes.Add(int64(n))
				return n, err
			}
		},
		ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(r io.Reader) (int64, error) {
				p.status.CompareAndSwap(0, http.StatusOK)
				n, err := next(r)
				p.bytes.Add(n)
				return n, err
			}
		},
		Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
			return func() { p.status.CompareAndSwap(0, http.StatusOK); next() }
		},
	})
}

// InstallErrorHandler wraps the Echo error handler; call after application setup.
func InstallErrorHandler(e *echo.Echo) {
	original := e.HTTPErrorHandler
	e.HTTPErrorHandler = func(c *echo.Context, err error) {
		panicked := true
		defer func() {
			if panicked {
				if p, ok := c.Get(spanKey).(*pending); ok {
					finish(c, p, true)
				}
			}
		}()
		if original != nil {
			original(c, err)
		} else {
			echo.DefaultHTTPErrorHandler(false)(c, err)
		}
		if p, ok := c.Get(spanKey).(*pending); ok {
			finish(c, p, false)
		}
		panicked = false
	}
}

// Install registers the middleware and wraps the configured error handler.
// Call after configuring the application's error handler and before serving.
func Install(e *echo.Echo, rec *flow.Recorder, configs ...Config) {
	InstallErrorHandler(e)
	e.Use(Middleware(rec, configs...))
}
func finish(c *echo.Context, p *pending, panicked bool) {
	if !p.finished.CompareAndSwap(false, true) {
		return
	}
	if p.original != nil {
		defer c.SetResponse(p.original)
	}
	req := c.Request()
	if panicked {
		status := p.status.Load()
		if status == 0 {
			status = http.StatusOK
		}
		p.span.End(flow.Observation{Method: req.Method, Route: "<unmatched>", Status: int(status), Bytes: p.bytes.Load(), Panicked: true})
		return
	}
	route := c.Path()
	if p.cfg.Route != nil {
		route = p.cfg.Route(c, route)
	}
	session := ""
	if p.cfg.Session != nil {
		session = p.cfg.Session(c)
	} else {
		session = p.rec.SessionID(req)
	}
	status := p.status.Load()
	if status == 0 {
		status = http.StatusOK
	}
	p.span.End(flow.Observation{Method: req.Method, Route: p.rec.RoutePattern(req, route), Session: session, Status: int(status), Bytes: p.bytes.Load(), Panicked: panicked})
}
