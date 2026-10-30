// Package fiberv2 adapts Fiber v2's fasthttp-backed contexts without building
// net/http request or response wrappers. Recorder's Session, Route, and Include
// options accept net/http requests and do not run here; use this package's
// Config callbacks instead. Recorder ExcludePaths is applied to native paths.
// Streamed response byte counts are marked unknown
// and the stream is left untouched.
// To capture returned errors, wrap the application's handler in
// fiber.Config{ErrorHandler: fiberv2.ErrorHandler(existing)} before app.Use.
package fiberv2

import (
	"strings"
	"sync/atomic"

	"github.com/gofiber/fiber/v2"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

const spanKey = "sessionflow.fiberv2.pending"

type Config struct {
	Session func(*fiber.Ctx) string
	Route   func(*fiber.Ctx, string) string
	// Include is evaluated for each request. When set, it replaces the default
	// /debug/ exclusion; compose that exclusion into this callback if needed.
	Include func(*fiber.Ctx) bool
}
type pending struct {
	span     *flow.Span
	rec      *flow.Recorder
	cfg      Config
	finished atomic.Bool
}

// Middleware records Fiber's route template and response. For returned errors,
// install the wrapped handler with fiber.New(Config{ErrorHandler: ErrorHandler(...)}).
func Middleware(rec *flow.Recorder, configs ...Config) fiber.Handler {
	cfg := Config{}
	if len(configs) > 0 {
		cfg = configs[0]
	}
	return func(c *fiber.Ctx) error {
		if !rec.Enabled() {
			return c.Next()
		}
		if cfg.Include != nil {
			if !cfg.Include(c) {
				return c.Next()
			}
		} else if strings.HasPrefix(c.Path(), "/debug/") {
			return c.Next()
		}
		span := rec.BeginPath(c.Path())
		if span == nil {
			return c.Next()
		}
		p := &pending{span: span, rec: rec, cfg: cfg}
		c.Locals(spanKey, p)
		completed := false
		defer func() {
			if !completed {
				finish(c, p, true)
			}
		}()
		err := c.Next()
		completed = true
		if err != nil {
			return err
		}
		finish(c, p, false)
		return nil
	}
}

// ErrorHandler decorates an application's Fiber error handler. It must be set
// in fiber.Config when creating the app so status/body are observed after writing.
func ErrorHandler(next fiber.ErrorHandler) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		handler := next
		if handler == nil {
			handler = fiber.DefaultErrorHandler
		}
		panicked := true
		defer func() {
			if panicked {
				if p, ok := c.Locals(spanKey).(*pending); ok {
					finish(c, p, true)
				}
			}
		}()
		out := handler(c, err)
		if p, ok := c.Locals(spanKey).(*pending); ok {
			finish(c, p, false)
		}
		panicked = false
		return out
	}
}
func finish(c *fiber.Ctx, p *pending, panicked bool) {
	if !p.finished.CompareAndSwap(false, true) {
		return
	}
	if panicked {
		response := c.Response()
		bytes, unknown := int64(0), response.IsBodyStream()
		if !unknown {
			bytes = int64(len(response.Body()))
		}
		p.span.End(flow.Observation{Method: c.Method(), Route: "<unmatched>", Status: response.StatusCode(), Bytes: bytes, BytesUnknown: unknown, Panicked: true})
		return
	}
	route := c.Route().Path
	if route == "/" && c.Path() != "/" {
		route = ""
	}
	if p.cfg.Route != nil {
		route = p.cfg.Route(c, route)
	}
	session := c.Cookies(p.rec.CookieName())
	if p.cfg.Session != nil {
		session = p.cfg.Session(c)
	}
	response := c.Response()
	bytes, unknown := int64(0), response.IsBodyStream()
	if !unknown {
		bytes = int64(len(response.Body()))
	}
	p.span.End(flow.Observation{Method: c.Method(), Route: route, Session: session,
		Status: response.StatusCode(), Bytes: bytes, BytesUnknown: unknown, Panicked: panicked})
}
