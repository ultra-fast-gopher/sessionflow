package bunrouter_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	flow "github.com/ultra-fast-gopher/sessionflow"
	adapter "github.com/ultra-fast-gopher/sessionflow/integration/bunrouter"
	bun "github.com/uptrace/bunrouter"
)

func TestMiddlewareNestedRouteAndErrors(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "bun", HashKey: []byte("shared"), MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	router := bun.New()
	api := router.NewGroup("/api", bun.Use(adapter.Middleware(r)))
	v1 := api.NewGroup("/v1")
	v1.GET("/users/:id", func(w http.ResponseWriter, req bun.Request) error {
		w.WriteHeader(201)
		_, _ = w.Write([]byte("ok"))
		return nil
	})
	req := httptest.NewRequest("GET", "/api/v1/users/private?token=secret", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "raw-session"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
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
	if len(s.Events) != 1 || s.Events[0].Route != "/api/v1/users/:id" || s.Events[0].Session == "raw-session" || s.Events[0].Status != 201 || s.Events[0].Bytes != 2 || s.Events[0].DurationNS <= 0 {
		t.Fatalf("events: %+v", s.Events)
	}
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "private") {
		t.Fatalf("leak: %s", data)
	}
}

func TestMiddlewareWithCustomErrorHandlerMeasuresFinalResponse(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "bun-error", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	original := errors.New("route failed")
	router := bun.New()
	group := router.NewGroup("", bun.Use(adapter.MiddlewareWithErrorHandler(r, func(w http.ResponseWriter, _ bun.Request, got error) error {
		if got != original {
			t.Errorf("error changed before callback: %v", got)
		}
		w.WriteHeader(418)
		_, _ = w.Write([]byte("teapot"))
		return got
	})))
	group.GET("/error", func(http.ResponseWriter, bun.Request) error { return original })
	if err := router.ServeHTTPError(httptest.NewRecorder(), httptest.NewRequest("GET", "/error", nil)); err != original {
		t.Fatalf("handler error changed: %v", err)
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Status != 418 || s.Events[0].Bytes != 6 || s.Events[0].DurationNS <= 0 {
		t.Fatalf("custom error response metrics: %+v", s.Events)
	}
}

func TestCustomErrorHandlerResponseIsSameWhenCaptureDisabled(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "bun-disabled", MaxEvents: 10})
	if err != nil {
		t.Fatal(err)
	}
	original := errors.New("route failed")
	var calls int
	router := bun.New()
	group := router.NewGroup("", bun.Use(adapter.MiddlewareWithErrorHandler(r, func(w http.ResponseWriter, _ bun.Request, got error) error {
		calls++
		if got != original {
			t.Errorf("error changed: %v", got)
		}
		w.WriteHeader(418)
		_, _ = w.Write([]byte("teapot"))
		return got
	})))
	group.GET("/error", func(http.ResponseWriter, bun.Request) error { return original })
	w := httptest.NewRecorder()
	if err := router.ServeHTTPError(w, httptest.NewRequest("GET", "/error", nil)); err != original {
		t.Fatalf("error changed: %v", err)
	}
	if calls != 1 || w.Code != 418 || w.Body.String() != "teapot" {
		t.Fatalf("calls=%d response=%d %q", calls, w.Code, w.Body.String())
	}
}

func TestHandlerPanicDoesNotRunRootMetadataCallbacks(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "bun-panic", HashKey: []byte("shared"), MaxEvents: 10,
		Route:   func(*http.Request) string { panic("metadata route") },
		Session: func(*http.Request) string { panic("metadata session") },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Start("capture")
	router := bun.New()
	group := router.NewGroup("", bun.Use(adapter.Middleware(r)))
	group.GET("/panic", func(http.ResponseWriter, bun.Request) error { panic("application") })
	req := httptest.NewRequest("GET", "/panic", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "raw-cookie"})
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		_ = router.ServeHTTPError(httptest.NewRecorder(), req)
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
	if len(s.Events) != 1 || !s.Events[0].Panicked || s.Events[0].Status != 0 || s.Events[0].Route != "/panic" || s.Events[0].Session == "raw-cookie" {
		t.Fatalf("panic event: %+v", s.Events)
	}
}
