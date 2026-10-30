// Package fasthttp adapts native fasthttp handlers without converting requests
// to net/http. Root Recorder Options.Session, Route and Include are net/http-only;
// use this package's Options callbacks instead. Streamed response byte counts
// are marked unknown and the stream is left untouched.
// Example: handler := fasthttp.Wrap(rec, appHandler, fasthttp.Options{Route: routeTemplate}).
package fasthttp

import (
	"bytes"

	flow "github.com/ultra-fast-gopher/sessionflow"
	"github.com/valyala/fasthttp"
)

type Options struct {
	// Route should return the router's template, such as /users/{id}. A nil
	// callback records <unmatched>; request paths are never used as routes.
	Route func(*fasthttp.RequestCtx) string
	// Session may return an authenticated identity. Nil reads Recorder.CookieName().
	Session func(*fasthttp.RequestCtx) string
	// Include replaces the default /debug/ exclusion when non-nil.
	Include func(*fasthttp.RequestCtx) bool
}

// Wrap records a fasthttp handler. Callback strings are copied by Span.End
// before fasthttp can reuse its request context.
func Wrap(rec *flow.Recorder, handler fasthttp.RequestHandler, options ...Options) fasthttp.RequestHandler {
	var cfg Options
	if len(options) > 0 {
		cfg = options[0]
	}
	return func(ctx *fasthttp.RequestCtx) {
		if !rec.Enabled() {
			handler(ctx)
			return
		}
		path := ctx.Path()
		if cfg.Include != nil {
			if !cfg.Include(ctx) {
				handler(ctx)
				return
			}
		} else if bytes.HasPrefix(path, []byte("/debug/")) {
			handler(ctx)
			return
		}
		span := rec.Begin()
		if span == nil {
			handler(ctx)
			return
		}
		panicked := true
		defer func() {
			if panicked {
				finish(rec, cfg, ctx, span, true)
			}
		}()
		handler(ctx)
		panicked = false
		finish(rec, cfg, ctx, span, false)
	}
}

func finish(rec *flow.Recorder, cfg Options, ctx *fasthttp.RequestCtx, span *flow.Span, panicked bool) {
	if panicked {
		response := &ctx.Response
		bytes, unknown := int64(0), response.IsBodyStream()
		if !unknown {
			bytes = int64(len(response.Body()))
		}
		span.End(flow.Observation{Method: string(ctx.Method()), Route: "<unmatched>", Status: response.StatusCode(), Bytes: bytes, BytesUnknown: unknown, Panicked: true})
		return
	}
	route := "<unmatched>"
	if cfg.Route != nil {
		route = cfg.Route(ctx)
	}
	session := ""
	if cfg.Session != nil {
		session = cfg.Session(ctx)
	} else {
		session = string(ctx.Request.Header.Cookie(rec.CookieName()))
	}
	response := &ctx.Response
	bytes, unknown := int64(0), response.IsBodyStream()
	if !unknown {
		bytes = int64(len(response.Body()))
	}
	span.End(flow.Observation{Method: string(ctx.Method()), Route: route, Session: session,
		Status: response.StatusCode(), Bytes: bytes, BytesUnknown: unknown, Panicked: panicked})
}
