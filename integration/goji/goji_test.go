package goji_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	flow "github.com/ultra-fast-gopher/sessionflow"
	adapter "github.com/ultra-fast-gopher/sessionflow/integration/goji"
	"goji.io"
	"goji.io/pat"
)

func TestMatchedPatternAndNestedPath(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "goji", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	mux := goji.NewMux()
	mux.Use(adapter.Middleware(r))
	mux.HandleFunc(pat.Get("/api/users/:id"), func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201); _, _ = w.Write([]byte("ok")) })
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/users/42", nil))
	if w.Code != 201 {
		t.Fatalf("status=%d", w.Code)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Route != "/api/users/:id" || s.Events[0].Status != 201 || s.Events[0].Bytes != 2 || s.Events[0].DurationNS <= 0 {
		t.Fatalf("events: %+v", s.Events)
	}
}
