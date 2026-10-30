package fasthttp

import (
	"io"
	"net"
	"strings"
	"testing"

	flow "github.com/ultra-fast-gopher/sessionflow"
	"github.com/valyala/fasthttp"
)

func TestWrapNativeRouteCookieAndUnknownFallback(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test", CookieName: "sid"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("capture"); err != nil {
		t.Fatal(err)
	}
	wrapped := Wrap(r, func(c *fasthttp.RequestCtx) { c.SetStatusCode(201); c.SetBodyString("ok") }, Options{Route: func(c *fasthttp.RequestCtx) string { return "/users/{id}" }})
	var req fasthttp.Request
	req.SetRequestURI("/users/alice?private=x")
	req.Header.SetMethod("GET")
	req.Header.SetCookie("sid", "secret")
	ctx := new(fasthttp.RequestCtx)
	ctx.Init(&req, &net.TCPAddr{}, nil)
	wrapped(ctx)
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 {
		t.Fatalf("events=%d", len(s.Events))
	}
	if err = s.Validate(); err != nil {
		t.Fatal(err)
	}
	e := s.Events[0]
	if e.Route != "/users/{id}" || e.Status != 201 || e.Bytes != 2 || e.Session == "" || e.Session == "secret" {
		t.Fatalf("observation=%+v", e)
	}
}

func TestWrapPanicsAndUsesUnknownRoute(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("capture"); err != nil {
		t.Fatal(err)
	}
	routeCalls, sessionCalls := 0, 0
	w := Wrap(r, func(*fasthttp.RequestCtx) { panic("boom") }, Options{Route: func(*fasthttp.RequestCtx) string { routeCalls++; return "/route" }, Session: func(*fasthttp.RequestCtx) string { sessionCalls++; return "session" }})
	var req fasthttp.Request
	req.SetRequestURI("/raw/private/path")
	ctx := new(fasthttp.RequestCtx)
	ctx.Init(&req, &net.TCPAddr{}, nil)
	var recovered any
	func() { defer func() { recovered = recover() }(); w(ctx) }()
	if recovered != "boom" {
		t.Fatalf("panic changed: %v", recovered)
	}
	if routeCalls != 0 || sessionCalls != 0 {
		t.Fatalf("callbacks route=%d session=%d", routeCalls, sessionCalls)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || !s.Events[0].Panicked || s.Events[0].Route != "<unmatched>" {
		t.Fatalf("events=%+v", s.Events)
	}
	if err = s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestWrapStreamingResponseIsNotConsumed(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("stream"); err != nil {
		t.Fatal(err)
	}
	w := Wrap(r, func(c *fasthttp.RequestCtx) {
		c.Response.SetBodyStream(io.NopCloser(strings.NewReader("streamed")), -1)
	}, Options{Route: func(*fasthttp.RequestCtx) string { return "/stream" }})
	var req fasthttp.Request
	req.SetRequestURI("/stream")
	ctx := new(fasthttp.RequestCtx)
	ctx.Init(&req, &net.TCPAddr{}, nil)
	w(ctx)
	if !ctx.Response.IsBodyStream() {
		t.Fatal("middleware consumed or cleared the response stream")
	}
	body := ctx.Response.Body()
	if string(body) != "streamed" {
		t.Fatalf("stream body=%q", body)
	}
	s, err := r.Stop("stream")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || !s.Events[0].BytesUnknown || s.Events[0].Bytes != 0 {
		t.Fatalf("stream event=%+v", s.Events)
	}
}
