package echov4

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

func TestMiddlewareRouteSessionAndReturnedError(t *testing.T) {
	rootCalls, nativeCalls := 0, 0
	r, err := flow.New(flow.Options{Source: "test", Session: func(*http.Request) string { rootCalls++; return "root" }})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("capture"); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	returned := errors.New("exact handler error")
	observed := false
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)
			if err == returned {
				observed = true
			}
			return err
		}
	})
	e.HTTPErrorHandler = func(err error, c echo.Context) { c.String(http.StatusTeapot, "error body") }
	InstallErrorHandler(e)
	e.Use(Middleware(r, Config{Session: func(c echo.Context) string { nativeCalls++; return c.Param("id") }}))
	group := e.Group("/api")
	group.GET("/users/:id", func(c echo.Context) error { return c.String(http.StatusCreated, "user") })
	e.GET("/fail/:id", func(c echo.Context) error { return returned })
	for _, tc := range []struct {
		method string
		path   string
		status int
		body   string
		route  string
	}{{http.MethodGet, "/api/users/alice", 201, "user", "/api/users/:id"}, {http.MethodGet, "/fail/bob", http.StatusTeapot, "error body", "/fail/:id"}, {http.MethodGet, "/missing/private", http.StatusTeapot, "error body", "<unmatched>"}, {http.MethodPost, "/api/users/alice", http.StatusTeapot, "error body", "<unmatched>"}} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status || w.Body.String() != tc.body {
			t.Fatalf("%s: got %d %q", tc.path, w.Code, w.Body.String())
		}
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 4 {
		t.Fatalf("events=%d", len(s.Events))
	}
	if !observed {
		t.Fatal("outer middleware did not receive the identical handler error")
	}
	if rootCalls != 0 || nativeCalls != 4 {
		t.Fatalf("root callback=%d native callback=%d", rootCalls, nativeCalls)
	}
	if err = s.Validate(); err != nil {
		t.Fatal(err)
	}
	for i, e := range s.Events {
		want := []string{"/api/users/:id", "/fail/:id", "<unmatched>", "/api/users/:id"}[i]
		if e.Route != want {
			t.Errorf("route=%q want %q", e.Route, want)
		}
		if e.Status != []int{201, 418, 418, 418}[i] || e.Bytes != []int64{4, 10, 10, 10}[i] || e.DurationNS <= 0 {
			t.Errorf("event=%+v", e)
		}
		if (i < 2 && e.Session == "") || strings.Contains(e.Session, "alice") || strings.Contains(e.Session, "bob") {
			t.Errorf("unsafe session hash %q", e.Session)
		}
	}
}

func TestConcurrentContextReuse(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test", MaxEvents: 128})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("concurrent"); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	Install(e, r)
	e.GET("/users/:id", func(c echo.Context) error { return c.String(200, "ok") })
	const n = 32
	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/secret", nil))
			if w.Code != 200 {
				t.Errorf("status=%d", w.Code)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	s, err := r.Stop("concurrent")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != n {
		t.Fatalf("events=%d", len(s.Events))
	}
	if err = s.Validate(); err != nil {
		t.Fatal(err)
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
	e := echo.New()
	Install(e, r, Config{Route: func(echo.Context, string) string { routeCalls++; return "/route" }, Session: func(echo.Context) string { sessionCalls++; return "session" }})
	e.GET("/panic", func(echo.Context) error { panic("original panic") })
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic", nil))
	}()
	if recovered != "original panic" {
		t.Fatalf("panic changed: %v", recovered)
	}
	if routeCalls != 0 || sessionCalls != 0 {
		t.Fatalf("route callback=%d session callback=%d", routeCalls, sessionCalls)
	}
	s, err := r.Stop("panic")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || !s.Events[0].Panicked {
		t.Fatalf("events=%+v", s.Events)
	}
}

func TestHostRouterUsesItsCachedRouteSet(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("host"); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	Install(e, r)
	e.Host("tenant.example").GET("/health", func(c echo.Context) error { return c.NoContent(204) })
	req := httptest.NewRequest(http.MethodGet, "http://tenant.example/health", nil)
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("status=%d", w.Code)
	}
	s, err := r.Stop("host")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Route != "/health" {
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
	first, second := echo.New(), echo.New()
	first.Use(mw)
	second.Use(mw)
	first.GET("/first", func(c echo.Context) error { return c.NoContent(204) })
	second.GET("/second", func(c echo.Context) error { return c.NoContent(204) })
	for e, path := range map[*echo.Echo]string{first: "/first", second: "/second"} {
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
