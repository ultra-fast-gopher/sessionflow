package fiberv2

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/recover"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

func TestMiddlewareTemplateCookieAndErrorHandler(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("capture"); err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(func(c *fiber.Ctx, err error) error { return c.Status(422).SendString("custom error") })})
	app.Use(Middleware(r))
	group := app.Group("/api")
	group.Get("/users/:id", func(c *fiber.Ctx) error { return c.Status(201).SendString("user") })
	app.Get("/fail/:id", func(c *fiber.Ctx) error { return fiber.NewError(400, "failure") })
	for _, tc := range []struct {
		method string
		path   string
		status int
		body   string
	}{{http.MethodGet, "/api/users/alice", 201, "user"}, {http.MethodGet, "/fail/bob", 422, "custom error"}, {http.MethodGet, "/missing/private", 422, "custom error"}, {http.MethodPost, "/api/users/alice", 422, "custom error"}} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "secret-session"})
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != tc.status {
			t.Fatalf("%s: status=%d", tc.path, resp.StatusCode)
		}
		buf, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if string(buf) != tc.body {
			t.Fatalf("body=%q", buf)
		}
	}
	s, err := r.Stop("capture")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 4 {
		t.Fatalf("events=%d", len(s.Events))
	}
	if err = s.Validate(); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"/api/users/:id", "/fail/:id", "<unmatched>", "<unmatched>"} {
		e := s.Events[i]
		if e.Route != want {
			t.Errorf("route=%q", e.Route)
		}
		if e.Status != []int{201, 422, 422, 422}[i] {
			t.Errorf("status=%d", e.Status)
		}
		if e.Bytes != int64(len([]string{"user", "custom error", "custom error", "custom error"}[i])) {
			t.Errorf("bytes=%d", e.Bytes)
		}
		if e.DurationNS <= 0 {
			t.Errorf("duration=%d", e.DurationNS)
		}
		if e.Session == "" || strings.Contains(e.Session, "secret-session") {
			t.Errorf("session was not hashed: %q", e.Session)
		}
	}
}

func TestDisabledSkipsNativeCallbacks(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	app := fiber.New()
	app.Use(Middleware(r, Config{Include: func(*fiber.Ctx) bool { calls++; return true }, Session: func(*fiber.Ctx) string { calls++; return "x" }}))
	app.Get("/x", func(c *fiber.Ctx) error { return c.SendString("ok") })
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/x", nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if calls != 0 {
		t.Fatalf("callbacks called while disabled: %d", calls)
	}
}

func TestStreamingResponseIsNotConsumed(t *testing.T) {
	r, err := flow.New(flow.Options{Source: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if err = r.Start("stream"); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	app.Use(Middleware(r))
	app.Get("/stream", func(c *fiber.Ctx) error {
		c.Response().SetBodyStream(io.NopCloser(strings.NewReader("streamed")), -1)
		return nil
	})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/stream", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "streamed" {
		t.Fatalf("stream body=%q", body)
	}
	s, err := r.Stop("stream")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || !s.Events[0].BytesUnknown || s.Events[0].Bytes != 0 {
		t.Fatalf("stream event=%+v", s.Events)
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
	app := fiber.New()
	app.Use(recover.New())
	app.Use(Middleware(r, Config{Route: func(*fiber.Ctx, string) string { routeCalls++; return "/route" }, Session: func(*fiber.Ctx) string { sessionCalls++; return "session" }}))
	app.Get("/panic", func(*fiber.Ctx) error { panic("original panic") })
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/panic", nil))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("status=%d", resp.StatusCode)
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
