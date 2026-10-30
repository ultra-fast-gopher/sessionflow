package gojiold

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ultra-fast-gopher/sessionflow"
	"github.com/zenazn/goji/web"
)

func TestMiddlewareCapturesMatchedHandler(t *testing.T) {
	rec, err := sessionflow.New(sessionflow.Options{Source: "goji-test", HashKey: []byte("test-key")})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Start("goji-capture"); err != nil {
		t.Fatal(err)
	}
	mux := web.New()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202); _, _ = w.Write([]byte("ok")) })
	mux.Get("/items/:id", Handler(rec, "/items/:id", web.HandlerFunc(func(c web.C, w http.ResponseWriter, r *http.Request) {
		if c.URLParams["id"] != "7" {
			t.Errorf("URLParams = %#v", c.URLParams)
		}
		handler.ServeHTTP(w, r)
	}), Config{Session: func(c web.C, r *http.Request) string {
		if c.URLParams["id"] != "7" || r == nil {
			t.Errorf("missing Goji context: %#v", c.URLParams)
		}
		return "private-session"
	}}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/items/7", nil))
	if w.Code != 202 || w.Body.String() != "ok" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	snap, err := rec.Stop("goji-capture")
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
	mux := web.New()
	mux.Get("/health", Handler(rec, "/health", web.HandlerFunc(func(_ web.C, w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }), Config{Session: func(web.C, *http.Request) string { called = true; return "native-session" }}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 || called {
		t.Fatalf("response=%d, session callback called=%v", w.Code, called)
	}
}

func TestHandlerTracksFinalStatusAfterInformationalResponse(t *testing.T) {
	rec, err := sessionflow.New(sessionflow.Options{Source: "goji-status-test", HashKey: []byte("test-key")})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Start("goji-status-capture"); err != nil {
		t.Fatal(err)
	}
	mux := web.New()
	mux.Get("/early", Handler(rec, "/early", web.HandlerFunc(func(_ web.C, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(103)
		w.WriteHeader(201)
		_, _ = w.Write([]byte("created"))
	})))
	server := httptest.NewServer(mux)
	defer server.Close()
	response, err := http.Get(server.URL + "/early")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if response.StatusCode != 201 || string(body) != "created" {
		t.Fatalf("response = %d %q", response.StatusCode, body)
	}
	snap, err := rec.Stop("goji-status-capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.Validate(); err != nil {
		t.Fatalf("invalid snapshot: %v", err)
	}
	if len(snap.Events) != 1 || snap.Events[0].Status != response.StatusCode || snap.Events[0].Bytes != int64(len(body)) {
		t.Fatalf("event = %#v", snap.Events)
	}
}
