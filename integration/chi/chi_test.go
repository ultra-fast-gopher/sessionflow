package chi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	chi "github.com/go-chi/chi/v5"
	flow "github.com/ultra-fast-gopher/sessionflow"
	adapter "github.com/ultra-fast-gopher/sessionflow/integration/chi"
)

func newRecorder(t *testing.T, opts flow.Options) *flow.Recorder {
	t.Helper()
	r, err := flow.New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start("capture"); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMiddlewareRoutePrivacyAndNestedGroups(t *testing.T) {
	r := newRecorder(t, flow.Options{Source: "chi", MaxEvents: 10})
	router := chi.NewRouter()
	router.Use(adapter.Middleware(r))
	router.Route("/api", func(api chi.Router) {
		api.Route("/v1", func(v1 chi.Router) {
			v1.Get("/users/{userID}", func(w http.ResponseWriter, req *http.Request) {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("ok"))
			})
		})
	})
	req := httptest.NewRequest("GET", "/api/v1/users/secret-id?token=secret", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "secret-cookie"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d", w.Code)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Route != "/api/v1/users/{userID}" || s.Events[0].Status != 201 || s.Events[0].Bytes != 2 || s.Events[0].DurationNS <= 0 || s.Events[0].Session == "secret-cookie" {
		t.Fatalf("snapshot: %+v", s)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "secret") {
		t.Fatalf("raw data leaked: %s", b)
	}
}

func TestMiddlewareCustomRouteOverrideAndMethodNotAllowed(t *testing.T) {
	type sessionKey struct{}
	r := newRecorder(t, flow.Options{Source: "chi", Route: func(*http.Request) string { return "/from-options" }, Session: func(req *http.Request) string { value, _ := req.Context().Value(sessionKey{}).(string); return value }})
	router := chi.NewRouter()
	router.Use(adapter.Middleware(r))
	router.Get("/known", func(http.ResponseWriter, *http.Request) {})
	req := httptest.NewRequest("POST", "/known", nil)
	req = req.WithContext(context.WithValue(req.Context(), sessionKey{}, "context-session"))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", w.Code)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Route != "/from-options" || s.Events[0].Session == "" {
		t.Fatalf("events: %+v", s.Events)
	}
}
