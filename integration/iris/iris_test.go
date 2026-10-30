package iris

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kataras/iris/v12"
	"github.com/ultra-fast-gopher/sessionflow"
)

func TestMiddlewareUsesResolvedRouteAndSession(t *testing.T) {
	rec, err := sessionflow.New(sessionflow.Options{Source: "iris-test", HashKey: []byte("test-key")})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Start("iris-capture"); err != nil {
		t.Fatal(err)
	}
	app := iris.New()
	app.UseRouter(Middleware(rec, Config{Session: func(ctx iris.Context) string { return ctx.Values().GetString("native-session") }}))
	app.Get("/users/{id:uint}", func(ctx iris.Context) {
		ctx.Values().Set("native-session", "private-session")
		ctx.StatusCode(201)
		_, _ = ctx.WriteString("ok")
	})
	if err := app.Build(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/users/42", nil))
	if w.Code != 201 || w.Body.String() != "ok" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/missing", nil))
	if w.Code != 404 {
		t.Fatalf("404 response = %d", w.Code)
	}
	snap, err := rec.Stop("iris-capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.Validate(); err != nil {
		t.Fatalf("invalid snapshot: %v", err)
	}
	if len(snap.Events) != 2 {
		t.Fatalf("events = %d", len(snap.Events))
	}
	e := snap.Events[0]
	if e.DurationNS <= 0 {
		t.Fatalf("duration = %d", e.DurationNS)
	}
	if e.Route != "/users/{id:uint}" || e.Status != 201 || e.Bytes != 2 || e.Session == "private-session" || len(e.Session) != 32 {
		t.Fatalf("event = %#v", e)
	}
	if snap.Events[1].Route != "<unmatched>" || snap.Events[1].Status != 404 {
		t.Fatalf("not found event = %#v", snap.Events[1])
	}
}

func TestDisabledSkipsNativeSessionCallbacks(t *testing.T) {
	called := false
	rec, err := sessionflow.New(sessionflow.Options{Session: func(*http.Request) string { called = true; return "root-session" }})
	if err != nil {
		t.Fatal(err)
	}
	app := iris.New()
	app.UseRouter(Middleware(rec, Config{Session: func(iris.Context) string { called = true; return "native-session" }}))
	app.Get("/health", func(ctx iris.Context) { _, _ = ctx.WriteString("ok") })
	if err := app.Build(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 || called {
		t.Fatalf("response=%d, session callback called=%v", w.Code, called)
	}
}
