package echov5

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

type readerOnly struct{ r *strings.Reader }

func (r readerOnly) Read(p []byte) (int, error) { return r.r.Read(p) }

type optionalWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *optionalWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *optionalWriter) WriteHeader(code int) {
	if code < 100 || code >= 200 || code == 101 {
		if w.status == 0 {
			w.status = code
		}
	}
}
func (w *optionalWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(b)
}
func (w *optionalWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.status == 0 {
		w.status = 200
	}
	return io.Copy(&w.body, r)
}
func (w *optionalWriter) Flush() {
	if w.status == 0 {
		w.status = 200
	}
}
func (w *optionalWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }

func TestResponseWriterPreservesInterfacesAndCountsReaderFrom(t *testing.T) {
	p := &pending{}
	base := &optionalWriter{}
	w := wrapResponseWriter(p, base)
	if _, ok := w.(http.Flusher); !ok {
		t.Fatal("Flusher interface lost")
	}
	if _, ok := w.(http.Hijacker); !ok {
		t.Fatal("Hijacker interface lost")
	}
	w.WriteHeader(103)
	w.WriteHeader(201)
	n, err := io.Copy(w, readerOnly{strings.NewReader("copied bytes")})
	if err != nil {
		t.Fatal(err)
	}
	if n != 12 || base.status != 201 || p.status.Load() != 201 || p.bytes.Load() != 12 {
		t.Fatalf("copied=%d status=%d captured=%d bytes=%d", n, base.status, p.status.Load(), p.bytes.Load())
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
	Install(e, r, Config{Route: func(*echo.Context, string) string { routeCalls++; return "/route" }, Session: func(*echo.Context) string { sessionCalls++; return "session" }})
	e.GET("/panic", func(*echo.Context) error { panic("original panic") })
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

func TestMiddlewareErrorHookAndRouteTemplate(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test", HashKey: []byte("test-key")})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("capture"); err != nil {
		t.Fatal(err)
	}
	e := echo.New()
	e.HTTPErrorHandler = func(c *echo.Context, err error) { _ = c.String(418, "custom error") }
	InstallErrorHandler(e)
	e.Use(Middleware(r, Config{Session: func(c *echo.Context) string { return c.Param("id") }}))
	group := e.Group("/api")
	group.GET("/users/:id", func(c *echo.Context) error { return c.String(201, "user") })
	e.GET("/fail/:id", func(c *echo.Context) error { return echo.NewHTTPError(400, "failure") })
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
