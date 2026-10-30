package beego

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beego/beego/v2/server/web"
	beectx "github.com/beego/beego/v2/server/web/context"
	"github.com/ultra-fast-gopher/sessionflow"
)

func TestFilterChainUsesMatchedTemplate(t *testing.T) {
	rec, err := sessionflow.New(sessionflow.Options{Source: "beego-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Start("beego-capture"); err != nil {
		t.Fatal(err)
	}
	app := web.NewControllerRegister()
	app.Get("/items/:id", func(ctx *beectx.Context) {
		ctx.Input.SetData("native-session", "private-session")
		ctx.ResponseWriter.WriteHeader(202)
		_, _ = ctx.ResponseWriter.Write([]byte("ok"))
	})
	app.InsertFilterChain("/*", FilterChain(rec, Config{Session: func(ctx *beectx.Context) string { v, _ := ctx.Input.GetData("native-session").(string); return v }}))
	app.Init()
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/items/7", nil))
	if w.Code != 202 || w.Body.String() != "ok" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/missing", nil))
	if w.Code != 404 {
		t.Fatalf("404 response = %d", w.Code)
	}
	snap, err := rec.Stop("beego-capture")
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
	if e.Route != "/items/:id" || e.Status != 202 || e.Bytes != 2 || e.Session == "private-session" || len(e.Session) != 64 {
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
	app := web.NewControllerRegister()
	app.Get("/health", func(ctx *beectx.Context) { _, _ = ctx.ResponseWriter.Write([]byte("ok")) })
	app.InsertFilterChain("/*", FilterChain(rec, Config{Session: func(*beectx.Context) string { called = true; return "native-session" }}))
	app.Init()
	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 || called {
		t.Fatalf("response=%d, session callback called=%v", w.Code, called)
	}
}

type readerOnly struct{ io.Reader }

func TestFilterChainCountsInformationalStatusAndReaderFrom(t *testing.T) {
	rec, err := sessionflow.New(sessionflow.Options{Source: "beego-copy-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Start("beego-copy-capture"); err != nil {
		t.Fatal(err)
	}
	app := web.NewControllerRegister()
	app.Get("/copy", func(ctx *beectx.Context) {
		ctx.ResponseWriter.WriteHeader(103)
		ctx.ResponseWriter.WriteHeader(201)
		_, _ = io.Copy(ctx.ResponseWriter, readerOnly{strings.NewReader("copy-body")})
	})
	app.InsertFilterChain("/*", FilterChain(rec))
	app.Init()
	server := httptest.NewServer(app)
	defer server.Close()
	response, err := http.Get(server.URL + "/copy")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != 200 || string(body) != "copy-body" {
		t.Fatalf("response = %d %q", response.StatusCode, body)
	}
	snap, err := rec.Stop("beego-copy-capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.Validate(); err != nil {
		t.Fatalf("invalid snapshot: %v", err)
	}
	if len(snap.Events) != 1 || snap.Events[0].Status != response.StatusCode || snap.Events[0].Bytes != int64(len("copy-body")) {
		t.Fatalf("event = %#v", snap.Events)
	}
}
