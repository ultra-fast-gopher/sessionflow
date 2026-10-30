package macaron

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ultra-fast-gopher/sessionflow"
	"gopkg.in/macaron.v1"
)

func TestMiddlewareCapturesExplicitRoutePattern(t *testing.T) {
	rec, err := sessionflow.New(sessionflow.Options{Source: "macaron-test", HashKey: []byte("test-key")})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Start("macaron-capture"); err != nil {
		t.Fatal(err)
	}
	app := macaron.New()
	app.Get("/items/:id", Middleware(rec, "/items/:id", Config{Session: func(ctx *macaron.Context) string {
		if ctx == nil {
			t.Fatal("missing native context")
		}
		return "private-session"
	}}), func(ctx *macaron.Context) { ctx.Resp.WriteHeader(202); _, _ = ctx.Resp.Write([]byte("ok")) })
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/items/7", nil))
	if w.Code != 202 || w.Body.String() != "ok" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	snap, err := rec.Stop("macaron-capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.Validate(); err != nil {
		t.Fatalf("invalid snapshot: %v", err)
	}
	if len(snap.Events) != 1 {
		t.Fatalf("events = %d", len(snap.Events))
	}
	e := snap.Events[0]
	if e.DurationNS <= 0 {
		t.Fatalf("duration = %d", e.DurationNS)
	}
	if e.Route != "/items/:id" || e.Status != 202 || e.Bytes != 2 || e.Session == "private-session" || len(e.Session) != 32 {
		t.Fatalf("event = %#v", e)
	}
}

func TestDisabledSkipsNativeSessionCallbacks(t *testing.T) {
	called := false
	rec, err := sessionflow.New(sessionflow.Options{Session: func(*http.Request) string { called = true; return "root-session" }})
	if err != nil {
		t.Fatal(err)
	}
	app := macaron.New()
	app.Get("/health", Middleware(rec, "/health", Config{Session: func(*macaron.Context) string { called = true; return "native-session" }}), func(ctx *macaron.Context) { _, _ = ctx.Resp.Write([]byte("ok")) })
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 || called {
		t.Fatalf("response=%d, session callback called=%v", w.Code, called)
	}
}
