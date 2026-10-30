package martini

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-martini/martini"
	"github.com/ultra-fast-gopher/sessionflow"
)

func TestMiddlewareCapturesRouteHandler(t *testing.T) {
	rec, err := sessionflow.New(sessionflow.Options{Source: "martini-test", HashKey: []byte("test-key")})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Start("martini-capture"); err != nil {
		t.Fatal(err)
	}
	app := martini.Classic()
	app.Get("/items/:id", Middleware(rec, Config{Session: func(ctx martini.Context, req *http.Request) string {
		if ctx == nil || req == nil {
			t.Fatal("missing native request context")
		}
		params := ctx.Get(reflect.TypeOf(martini.Params{}))
		if !params.IsValid() || params.Interface().(martini.Params)["id"] != "7" {
			t.Fatal("native route params unavailable")
		}
		return "private-session"
	}}), func(w http.ResponseWriter) { w.WriteHeader(202); _, _ = w.Write([]byte("ok")) })
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/items/7", nil))
	if w.Code != 202 || w.Body.String() != "ok" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	snap, err := rec.Stop("martini-capture")
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

func TestDisabledDoesNotCallSession(t *testing.T) {
	called := false
	rec, err := sessionflow.New(sessionflow.Options{Session: func(*http.Request) string { called = true; return "secret" }})
	if err != nil {
		t.Fatal(err)
	}
	app := martini.Classic()
	app.Get("/items/:id", Middleware(rec), func(http.ResponseWriter) {})
	app.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/items/1", nil))
	if called {
		t.Fatal("session callback called while capture disabled")
	}
}
