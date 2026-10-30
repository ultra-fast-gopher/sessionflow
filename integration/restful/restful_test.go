package restful_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rf "github.com/emicklei/go-restful/v3"
	flow "github.com/ultra-fast-gopher/sessionflow"
	adapter "github.com/ultra-fast-gopher/sessionflow/integration/restful"
)

type sessionKey struct{}

func TestContainerFilterRouteStatusBytesAndSession(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "restful", HashKey: []byte("shared"), MaxEvents: 10, Session: func(req *http.Request) string { value, _ := req.Context().Value(sessionKey{}).(string); return value }})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	container := rf.NewContainer()
	container.Filter(adapter.Filter(r))
	ws := new(rf.WebService).Path("/api")
	ws.Route(ws.GET("/users/{id}").To(func(req *rf.Request, resp *rf.Response) {
		if got := req.PathParameter("id"); got != "private-id" {
			t.Errorf("path parameter=%q", got)
		}
		resp.WriteHeader(202)
		_, _ = resp.Write([]byte("ok"))
	}))
	ws.Route(ws.GET("/fail").To(func(_ *rf.Request, resp *rf.Response) { _ = resp.WriteErrorString(418, "teapot") }))
	container.Add(ws)
	req := httptest.NewRequest("GET", "/api/users/private-id?token=secret", nil)
	req = req.WithContext(context.WithValue(req.Context(), sessionKey{}, "context-secret"))
	req.AddCookie(&http.Cookie{Name: "session", Value: "cookie-secret"})
	w := httptest.NewRecorder()
	container.ServeHTTP(w, req)
	if w.Code != 202 {
		t.Fatalf("status=%d", w.Code)
	}
	errorResponse := httptest.NewRecorder()
	container.ServeHTTP(errorResponse, httptest.NewRequest("GET", "/api/fail", nil))
	if errorResponse.Code != 418 || errorResponse.Body.String() != "teapot" {
		t.Fatalf("error response=%d %q", errorResponse.Code, errorResponse.Body.String())
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 2 || s.Events[0].Route != "/api/users/{id}" || s.Events[0].Status != 202 || s.Events[0].Bytes != 2 || s.Events[0].DurationNS <= 0 || s.Events[0].Session == "context-secret" || s.Events[1].Route != "/api/fail" || s.Events[1].Status != 418 || s.Events[1].Bytes != 6 || s.Events[1].DurationNS <= 0 {
		t.Fatalf("events: %+v", s.Events)
	}
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "private-id") || strings.Contains(string(data), "secret") {
		t.Fatalf("leak: %s", data)
	}
}

func TestUnmatchedRouteUsesServeMux404OutsideFilterCapture(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "restful", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	container := rf.NewContainer()
	container.Filter(adapter.Filter(r))
	w := httptest.NewRecorder()
	container.ServeHTTP(w, httptest.NewRequest("GET", "/missing/secret", nil))
	if w.Code != 404 {
		t.Fatalf("response=%d %q", w.Code, w.Body.String())
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 0 || s.Seen != 0 {
		t.Fatalf("unmatched route unexpectedly recorded: %+v", s)
	}
}

func TestHandlerPanicDoesNotRunMetadataCallbacks(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "restful-panic", HashKey: []byte("shared"), MaxEvents: 10,
		Route:   func(*http.Request) string { panic("metadata route") },
		Session: func(*http.Request) string { panic("metadata session") },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	container := rf.NewContainer()
	container.Filter(adapter.Filter(r))
	ws := new(rf.WebService).Path("/api")
	ws.Route(ws.GET("/panic").To(func(*rf.Request, *rf.Response) { panic("application") }))
	container.Add(ws)
	req := httptest.NewRequest("GET", "/api/panic", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "raw-cookie"})
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		container.ServeHTTP(httptest.NewRecorder(), req)
	}()
	if recovered != "application" {
		t.Fatalf("recovered panic=%v", recovered)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || !s.Events[0].Panicked || s.Events[0].Status != 0 || s.Events[0].Route != "/api/panic" || s.Events[0].Session == "raw-cookie" {
		t.Fatalf("panic event: %+v", s.Events)
	}
}
