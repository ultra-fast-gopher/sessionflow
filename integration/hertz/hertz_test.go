package hertz_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/route"
	flow "github.com/ultra-fast-gopher/sessionflow"
	adapter "github.com/ultra-fast-gopher/sessionflow/integration/hertz"
)

type countReader struct{ reads int }

func (r *countReader) Read(p []byte) (int, error) {
	r.reads++
	return copy(p, "stream"), nil
}

func newEngine(t *testing.T, r *flow.Recorder, cfg ...adapter.Config) *route.Engine {
	t.Helper()
	e := route.NewEngine(config.NewOptions(nil))
	e.Use(adapter.Middleware(r, cfg...))
	return e
}

func prepare(ctx *app.RequestContext, method, target, session string) {
	ctx.Reset()
	protocol.NewRequest(method, target, nil).CopyTo(&ctx.Request)
	ctx.Request.SetHost("example.test")
	if session != "" {
		ctx.Request.Header.SetCookie("session", session)
	}
}

func TestMiddlewareNativeSessionRouteStatusAndBytes(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "hertz", HashKey: []byte("shared"), MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	cfg := adapter.Config{Session: func(_ context.Context, c *app.RequestContext) string {
		value, ok := c.Get("session")
		if !ok {
			return ""
		}
		return string(value.([]byte))
	}}
	e := newEngine(t, r, cfg)
	group := e.Group("/api")
	group.GET("/users/:id", func(_ context.Context, c *app.RequestContext) {
		if got := c.Param("id"); got != "private-id" {
			t.Errorf("path parameter=%q", got)
		}
		c.SetStatusCode(201)
		_, _ = c.Write([]byte("ok"))
	})
	reqCtx := e.NewContext()
	prepare(reqCtx, "GET", "/api/users/private-id?token=secret", "cookie-secret")
	reqCtx.Set("session", []byte("native-session-secret"))
	e.ServeHTTP(context.Background(), reqCtx)
	if reqCtx.Response.StatusCode() != 201 {
		t.Fatalf("status=%d", reqCtx.Response.StatusCode())
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Route != "/api/users/:id" || s.Events[0].Status != 201 || s.Events[0].Bytes != 2 || s.Events[0].DurationNS <= 0 || s.Events[0].Session == "native-session-secret" || len(s.Events[0].Session) != 32 {
		t.Fatalf("events: %+v", s.Events)
	}
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "private-id") || strings.Contains(string(data), "secret") {
		t.Fatalf("leak: %s", data)
	}
}

func TestMiddlewareContextReuseCopiesSessionAndKeepsRoutes(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "hertz", HashKey: []byte("shared"), MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	e := newEngine(t, r, adapter.Config{Session: func(_ context.Context, c *app.RequestContext) string {
		value, _ := c.Get("session")
		return string(value.([]byte))
	}})
	e.GET("/users/:id", func(_ context.Context, c *app.RequestContext) { c.Write([]byte("x")) })
	ctx := e.NewContext()
	for _, tc := range []struct{ id, session string }{{"one", "session-one"}, {"two", "session-two"}} {
		prepare(ctx, "GET", "/users/"+tc.id, "")
		ctx.Set("session", []byte(tc.session))
		e.ServeHTTP(context.Background(), ctx)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 2 || s.Events[0].Route != "/users/:id" || s.Events[1].Route != "/users/:id" || s.Events[0].Session == s.Events[1].Session || s.Events[0].Bytes != 1 || s.Events[1].Bytes != 1 {
		t.Fatalf("reused context events: %+v", s.Events)
	}
}

func TestMiddlewareStreamAndNoRoute(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "hertz", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	e := newEngine(t, r)
	stream := &countReader{}
	e.GET("/stream", func(_ context.Context, c *app.RequestContext) {
		c.Response.SetBodyStream(stream, 6)
	})
	streamReq := e.NewContext()
	prepare(streamReq, "GET", "/stream", "")
	e.ServeHTTP(context.Background(), streamReq)
	missingReq := e.NewContext()
	prepare(missingReq, "GET", "/missing/private", "")
	e.ServeHTTP(context.Background(), missingReq)
	if missingReq.Response.StatusCode() != http.StatusNotFound {
		t.Fatalf("404 status=%d", missingReq.Response.StatusCode())
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 2 || !s.Events[0].BytesUnknown || s.Events[0].Bytes != 0 || s.Events[0].Route != "/stream" || s.Events[1].Route != "<unmatched>" || s.Events[1].Status != http.StatusNotFound {
		t.Fatalf("stream/404 events: %+v", s.Events)
	}
	if stream.reads != 0 {
		t.Fatalf("stream body consumed %d times", stream.reads)
	}
}

func TestIncludeNotCalledWhileDisabled(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "hertz", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	e := newEngine(t, r, adapter.Config{Include: func(context.Context, *app.RequestContext) bool { called++; return true }})
	e.GET("/health", func(_ context.Context, c *app.RequestContext) { c.SetStatusCode(204) })
	ctx := e.NewContext()
	prepare(ctx, "GET", "/health", "")
	e.ServeHTTP(context.Background(), ctx)
	if called != 0 {
		t.Fatalf("Include called while recording disabled: %d", called)
	}
	_ = r.Start("capture")
	ctx = e.NewContext()
	prepare(ctx, "GET", "/health", "")
	e.ServeHTTP(context.Background(), ctx)
	if called != 1 {
		t.Fatalf("Include calls=%d", called)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if s.Seen != 1 || len(s.Events) != 1 {
		t.Fatalf("capture: %+v", s)
	}
}

func TestDefaultDebugExclusion(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "hertz", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	e := newEngine(t, r)
	e.GET("/debug/health", func(_ context.Context, c *app.RequestContext) { c.SetStatusCode(204) })
	ctx := e.NewContext()
	prepare(ctx, "GET", "/debug/health", "")
	e.ServeHTTP(context.Background(), ctx)
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if s.Seen != 0 || len(s.Events) != 0 {
		t.Fatalf("debug route captured: %+v", s)
	}
}

func TestHandlerPanicDoesNotRunMetadataCallbacks(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "hertz-panic", HashKey: []byte("shared"), MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	cfg := adapter.Config{
		Session: func(context.Context, *app.RequestContext) string { panic("metadata session") },
		Route:   func(context.Context, *app.RequestContext, string) string { panic("metadata route") },
	}
	e := newEngine(t, r, cfg)
	e.GET("/panic", func(_ context.Context, _ *app.RequestContext) { panic("application") })
	ctx := e.NewContext()
	prepare(ctx, "GET", "/panic", "raw-cookie")
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		e.ServeHTTP(context.Background(), ctx)
	}()
	if recovered != "application" {
		t.Fatalf("recovered panic=%v", recovered)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || !s.Events[0].Panicked || s.Events[0].Status != 0 || s.Events[0].Route != "/panic" || s.Events[0].Session == "raw-cookie" {
		t.Fatalf("panic event: %+v", s.Events)
	}
}
