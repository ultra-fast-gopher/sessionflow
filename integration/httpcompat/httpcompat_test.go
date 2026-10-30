package httpcompat_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/justinas/alice"
	flow "github.com/ultra-fast-gopher/sessionflow"
	"github.com/urfave/negroni"
)

func TestAliceMiddlewareChain(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "alice", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("alice chain lost Flusher")
		}
		w.WriteHeader(202)
		_, _ = w.Write([]byte("ok"))
	})
	h := alice.New(r.Wrap).Then(next)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/items/42", nil))
	if w.Code != 202 {
		t.Fatalf("status=%d", w.Code)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Status != 202 || s.Events[0].Bytes != 2 {
		t.Fatalf("events: %+v", s.Events)
	}
}

func TestNegroniHandlerChain(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "negroni", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	n := negroni.New()
	n.UseHandler(r.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("negroni chain lost Flusher")
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	w := httptest.NewRecorder()
	n.ServeHTTP(w, httptest.NewRequest("GET", "/items/42", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d", w.Code)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Status != http.StatusNoContent {
		t.Fatalf("events: %+v", s.Events)
	}
}
