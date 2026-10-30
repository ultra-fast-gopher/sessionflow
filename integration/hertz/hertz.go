// Package hertz adapts CloudWeGo Hertz contexts to sessionflow without
// converting requests or responses to net/http.
package hertz

import (
	"bytes"
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

// Config contains callbacks for values stored in the native Hertz context.
// Recorder Options.Session, Route, and Include are net/http-only and are not
// invoked by this adapter. Recorder ExcludePaths is applied to native paths.
type Config struct {
	Session func(context.Context, *app.RequestContext) string
	Route   func(context.Context, *app.RequestContext, string) string
	// Include replaces the default /debug/ exclusion when non-nil.
	Include func(context.Context, *app.RequestContext) bool
}

// Middleware returns an app.HandlerFunc for Engine.Use. It preserves Hertz
// context values for callbacks and never consumes streamed response bodies.
func Middleware(rec *flow.Recorder, configs ...Config) app.HandlerFunc {
	var cfg Config
	if len(configs) > 0 {
		cfg = configs[0]
	}
	return func(ctx context.Context, c *app.RequestContext) {
		if !rec.Enabled() {
			c.Next(ctx)
			return
		}
		if cfg.Include != nil {
			if !cfg.Include(ctx, c) {
				c.Next(ctx)
				return
			}
		} else if bytes.HasPrefix(c.Path(), []byte("/debug/")) {
			c.Next(ctx)
			return
		}
		span := rec.BeginPath(string(c.Path()))
		if span == nil {
			c.Next(ctx)
			return
		}
		completed := false
		defer func() {
			finish(rec, cfg, ctx, c, span, !completed)
		}()
		c.Next(ctx)
		completed = true
	}
}

func finish(rec *flow.Recorder, cfg Config, ctx context.Context, c *app.RequestContext, span *flow.Span, panicked bool) {
	method := string(c.Method())
	route := c.FullPath()
	session := string(c.Cookie(rec.CookieName()))
	if !panicked {
		if cfg.Route != nil {
			route = cfg.Route(ctx, c, route)
		}
		if cfg.Session != nil {
			session = cfg.Session(ctx, c)
		}
	}
	bytes, unknown := int64(0), c.Response.IsBodyStream()
	if !unknown {
		bytes = int64(len(c.Response.BodyBytes()))
	}
	span.End(flow.Observation{Method: method, Route: route, Session: session,
		Status: c.Response.StatusCode(), Bytes: bytes, BytesUnknown: unknown, Panicked: panicked})
}
