package echo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	framework "github.com/labstack/echo"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

func TestMiddlewareErrorHookAndRouteTemplate(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test", HashKey: []byte("test-key")})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("capture"); err != nil {
		t.Fatal(err)
	}
	e := framework.New()
	e.HTTPErrorHandler = func(err error, c framework.Context) { _ = c.String(418, "custom error") }
	InstallErrorHandler(e)
	e.Use(Middleware(r, Config{Session: func(c framework.Context) string { return c.Param("id") }}))
	group := e.Group("/api")
	group.GET("/users/:id", func(c framework.Context) error { return c.String(201, "user") })
	e.GET("/fail/:id", func(c framework.Context) error { return framework.NewHTTPError(400, "failure") })
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{{"/api/users/alice", 201, "user"}, {"/fail/bob", 418, "custom error"}, {"/missing/private", 418, "custom error"}} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if w.Code != tc.status || w.Body.String() != tc.body {
			t.Fatalf("%s: got %d %q", tc.path, w.Code, w.Body.String())
		}
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 3 {
		t.Fatalf("events=%d", len(s.Events))
	}
	if err = s.Validate(); err != nil {
		t.Fatal(err)
	}
	for i, e := range s.Events {
		if e.Status != []int{201, 418, 418}[i] || e.Bytes != []int64{4, 12, 12}[i] || e.DurationNS <= 0 {
			t.Errorf("event=%+v", e)
		}
	}
	if s.Events[0].Route != "/api/users/:id" || s.Events[1].Route != "/fail/:id" || s.Events[2].Route != "<unmatched>" {
		t.Fatalf("routes=%q,%q,%q", s.Events[0].Route, s.Events[1].Route, s.Events[2].Route)
	}
}

func TestPanickingHandlerSkipsUserCallbacks(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("panic"); err != nil {
		t.Fatal(err)
	}
	routeCalls, sessionCalls := 0, 0
	e := framework.New()
	Install(e, r, Config{Route: func(framework.Context, string) string { routeCalls++; return "/route" }, Session: func(framework.Context) string { sessionCalls++; return "session" }})
	e.GET("/panic", func(framework.Context) error { panic("original panic") })
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic", nil))
	}()
	if recovered != "original panic" {
		t.Fatalf("panic changed: %v", recovered)
	}
	if routeCalls != 0 || sessionCalls != 0 {
		t.Fatalf("callbacks route=%d session=%d", routeCalls, sessionCalls)
	}
	s, err := r.Stop("panic")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || !s.Events[0].Panicked {
		t.Fatalf("events=%+v", s.Events)
	}
}

func TestSharedMiddlewareCachesRoutesPerEchoInstance(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("instances"); err != nil {
		t.Fatal(err)
	}
	mw := Middleware(r)
	first, second := framework.New(), framework.New()
	first.Use(mw)
	second.Use(mw)
	first.GET("/first", func(c framework.Context) error { return c.NoContent(204) })
	second.GET("/second", func(c framework.Context) error { return c.NoContent(204) })
	for e, path := range map[*framework.Echo]string{first: "/first", second: "/second"} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != 204 {
			t.Fatalf("%s status=%d", path, w.Code)
		}
	}
	s, err := r.Stop("instances")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 2 {
		t.Fatalf("events=%+v", s.Events)
	}
	got := map[string]bool{}
	for _, event := range s.Events {
		got[event.Route] = true
	}
	if !got["/first"] || !got["/second"] {
		t.Fatalf("routes=%v", got)
	}
}
