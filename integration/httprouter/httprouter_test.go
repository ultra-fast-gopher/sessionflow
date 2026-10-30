package httprouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	router "github.com/julienschmidt/httprouter"
	flow "github.com/ultra-fast-gopher/sessionflow"
	adapter "github.com/ultra-fast-gopher/sessionflow/integration/httprouter"
)

func TestRegisteredTemplateAndPrivacy(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "httprouter", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	mux := router.New()
	mux.Handle("GET", "/users/:id", adapter.Handle(r, "/users/:id", func(w http.ResponseWriter, _ *http.Request, params router.Params) {
		if got := params.ByName("id"); got != "secret-id" {
			t.Errorf("route param = %q", got)
		}
		w.WriteHeader(202)
		_, _ = w.Write([]byte("ok"))
	}))
	req := httptest.NewRequest("GET", "/users/secret-id?token=secret", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "secret-cookie"})
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
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
	if len(s.Events) != 1 || s.Events[0].Route != "/users/:id" || s.Events[0].Session == "secret-cookie" || s.Events[0].Status != 202 || s.Events[0].Bytes != 2 || s.Events[0].DurationNS <= 0 {
		t.Fatalf("events: %+v", s.Events)
	}
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "secret") {
		t.Fatalf("leak: %s", data)
	}
}

func TestGenericMiddlewareDoesNotGuessRouteFromURL(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "httprouter", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	h := adapter.Middleware(r)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/users/42", nil))
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Route != "<unmatched>" {
		t.Fatalf("event: %+v", s.Events)
	}
}
