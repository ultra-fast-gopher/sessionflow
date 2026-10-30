package gozero_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	flow "github.com/ultra-fast-gopher/sessionflow"
	adapter "github.com/ultra-fast-gopher/sessionflow/integration/gozero"
	"github.com/zeromicro/go-zero/rest/pathvar"
	"github.com/zeromicro/go-zero/rest/router"
)

type sessionKey struct{}

func TestRouteHandlerTemplatesParamsAndSessionContext(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "go-zero", HashKey: []byte("shared"), MaxEvents: 10, Session: func(req *http.Request) string { value, _ := req.Context().Value(sessionKey{}).(string); return value }})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	rt := router.NewRouter()
	handler := adapter.Handler(r, "/api/users/:id", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if got := pathvar.Vars(req)["id"]; got != "secret" {
			t.Errorf("path param=%q", got)
		}
		w.WriteHeader(202)
		_, _ = w.Write([]byte("ok"))
	}))
	if err := rt.Handle("GET", "/api/users/:id", handler); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/users/secret?token=private", nil)
	req = req.WithContext(context.WithValue(req.Context(), sessionKey{}, "context-session"))
	req.AddCookie(&http.Cookie{Name: "session", Value: "cookie-secret"})
	w := httptest.NewRecorder()
	rt.ServeHTTP(w, req)
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
	if len(s.Events) != 1 || s.Events[0].Route != "/api/users/:id" || s.Events[0].Status != 202 || s.Events[0].Bytes != 2 || s.Events[0].DurationNS <= 0 || s.Events[0].Session == "context-session" {
		t.Fatalf("events: %+v", s.Events)
	}
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "private") {
		t.Fatalf("leak: %s", data)
	}
}

func TestGenericMiddlewareCaptures404WithoutGuessingTemplate(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "go-zero", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	rt := router.NewRouter()
	h := adapter.Middleware(r)(rt.ServeHTTP)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/missing/secret", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d", w.Code)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Route != "<unmatched>" || s.Events[0].Status != http.StatusNotFound {
		t.Fatalf("events: %+v", s.Events)
	}
}
