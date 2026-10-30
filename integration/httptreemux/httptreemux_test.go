package httptreemux_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	treemux "github.com/dimfeld/httptreemux/v5"
	flow "github.com/ultra-fast-gopher/sessionflow"
	adapter "github.com/ultra-fast-gopher/sessionflow/integration/httptreemux"
)

func TestContextRouteNestedGroupAndMethodNotAllowed(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "treemux", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	router := treemux.New()
	group := router.NewGroup("/api")
	cg := group.UsingContext()
	cg.UseHandler(adapter.Middleware(r))
	cg.GET("/items/:id", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202); _, _ = w.Write([]byte("ok")) })
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/items/42", nil))
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
	if len(s.Events) != 1 || s.Events[0].Route != "/api/items/:id" || s.Events[0].Status != 202 || s.Events[0].Bytes != 2 || s.Events[0].DurationNS <= 0 {
		t.Fatalf("events: %+v", s.Events)
	}
}

func TestMethodNotAllowedResponse(t *testing.T) {
	router := treemux.New()
	router.GET("/items/:id", func(http.ResponseWriter, *http.Request, map[string]string) {})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("POST", "/items/1", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", w.Code)
	}
}
