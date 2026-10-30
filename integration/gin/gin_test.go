package gin

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

func TestMiddlewareTemplateSessionAndUnmatched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rootCalls, nativeCalls := 0, 0
	r, err := flow.New(flow.Options{Source: "test", Session: func(*http.Request) string { rootCalls++; return "root" }})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("capture"); err != nil {
		t.Fatal(err)
	}
	e := gin.New()
	e.Use(Middleware(r, Config{Session: func(c *gin.Context) string { nativeCalls++; return c.Param("id") }}))
	e.GET("/users/:id", func(c *gin.Context) { c.String(http.StatusCreated, "user") })
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{{"/users/alice", 201, "user"}, {"/missing/secret", 404, "404 page not found"}} {
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
	if len(s.Events) != 2 {
		t.Fatalf("events=%d", len(s.Events))
	}
	if err = s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.Events[0].Status != 201 || s.Events[0].Bytes != 4 || s.Events[0].DurationNS <= 0 {
		t.Errorf("matched event=%+v", s.Events[0])
	}
	if s.Events[1].Status != 404 || s.Events[1].Bytes < 0 || s.Events[1].DurationNS <= 0 {
		t.Errorf("unmatched event=%+v", s.Events[1])
	}
	if s.Events[0].Route != "/users/:id" || s.Events[0].Session == "" {
		t.Fatalf("route/session=%+v", s.Events[0])
	}
	if s.Events[1].Route != "<unmatched>" {
		t.Fatalf("unmatched route=%q", s.Events[1].Route)
	}
	if rootCalls != 0 || nativeCalls != 2 {
		t.Fatalf("root callback=%d native callback=%d", rootCalls, nativeCalls)
	}
	if s.Events[1].Bytes < 0 {
		t.Fatalf("negative byte count: %d", s.Events[1].Bytes)
	}
}

func TestConcurrentContextReuse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, err := flow.New(flow.Options{Source: "test", MaxEvents: 128})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("concurrent"); err != nil {
		t.Fatal(err)
	}
	e := gin.New()
	e.Use(Middleware(r))
	group := e.Group("/api")
	group.GET("/users/:id", func(c *gin.Context) { c.String(200, "ok") })
	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users/secret", nil))
			if w.Code != 200 {
				t.Errorf("status=%d", w.Code)
			}
		}()
	}
	wg.Wait()
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
	for _, e := range s.Events {
		if e.Route != "/api/users/:id" {
			t.Errorf("route=%q", e.Route)
		}
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
	e := gin.New()
	e.Use(Middleware(r, Config{Route: func(*gin.Context, string) string { routeCalls++; return "/route" }, Session: func(*gin.Context) string { sessionCalls++; return "session" }}))
	e.GET("/panic", func(*gin.Context) { panic("original panic") })
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
