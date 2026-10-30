package gorillamux_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	flow "github.com/ultra-fast-gopher/sessionflow"
	adapter "github.com/ultra-fast-gopher/sessionflow/integration/gorillamux"
)

func TestMiddlewareTemplatesGroupsAndPrivacy(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "mux", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("capture"); err != nil {
		t.Fatal(err)
	}
	router := mux.NewRouter()
	router.Use(adapter.Middleware(r))
	api := router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/users/{id:[0-9]+}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202); _, _ = w.Write([]byte("mux")) }).Methods("GET")
	req := httptest.NewRequest("GET", "/api/users/42?token=secret", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "cookie-secret"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 202 {
		t.Fatalf("status=%d", w.Code)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Route != "/api/users/{id:[0-9]+}" || s.Events[0].Session == "cookie-secret" || s.Events[0].Status != 202 || s.Events[0].Bytes != 3 || s.Events[0].DurationNS <= 0 {
		t.Fatalf("events: %+v", s.Events)
	}
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "/42") {
		t.Fatalf("leak: %s", data)
	}
}

func TestMiddlewareNotFoundAndMethodNotAllowed(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "mux", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	router := mux.NewRouter()
	router.Use(adapter.Middleware(r))
	router.HandleFunc("/items/{id}", func(http.ResponseWriter, *http.Request) {}).Methods("GET")
	for _, tt := range []struct {
		method, path string
		status       int
	}{{"GET", "/missing", 404}, {"POST", "/items/1", 405}} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
		if w.Code != tt.status {
			t.Fatalf("%s %s status=%d", tt.method, tt.path, w.Code)
		}
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if s.Seen != 0 || len(s.Events) != 0 {
		t.Fatalf("events: %+v", s)
	}
}

func TestInstallCapturesMatched404And405ExactlyOnce(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "mux-install", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	router := mux.NewRouter()
	router.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(404); _, _ = w.Write([]byte("missing")) })
	router.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(405); _, _ = w.Write([]byte("wrong")) })
	adapter.Install(router, r)
	router.HandleFunc("/items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201); _, _ = w.Write([]byte("ok")) }).Methods("GET")
	for _, tc := range []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/items/1", 201, "ok"},
		{"GET", "/missing", 404, "missing"},
		{"POST", "/items/1", 405, "wrong"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || w.Body.String() != tc.body {
			t.Fatalf("%s %s => %d %q", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		route  string
		status int
		bytes  int64
	}{{"/items/{id}", 201, 2}, {"<unmatched>", 404, 7}, {"<unmatched>", 405, 5}}
	if s.Seen != 3 || len(s.Events) != len(want) {
		t.Fatalf("capture count: %+v", s)
	}
	for i, e := range s.Events {
		if e.Route != want[i].route || e.Status != want[i].status || e.Bytes != want[i].bytes || e.DurationNS <= 0 {
			t.Fatalf("event %d: %+v", i, e)
		}
	}
}
