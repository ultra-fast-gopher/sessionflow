package chiv4_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	chi "github.com/go-chi/chi"
	flow "github.com/ultra-fast-gopher/sessionflow"
	adapter "github.com/ultra-fast-gopher/sessionflow/integration/chiv4"
)

func TestNestedRouteAndStatus(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "chi-v4", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	router := chi.NewRouter()
	router.Use(adapter.Middleware(r))
	router.Route("/api", func(api chi.Router) {
		api.Route("/users", func(users chi.Router) {
			users.Get("/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202); _, _ = w.Write([]byte("ok")) })
		})
	})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/users/42", nil))
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
	if len(s.Events) != 1 || s.Events[0].Route != "/api/users/{id}" || s.Events[0].Status != 202 || s.Events[0].Bytes != 2 || s.Events[0].DurationNS <= 0 {
		t.Fatalf("events: %+v", s.Events)
	}
}
