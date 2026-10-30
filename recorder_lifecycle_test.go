package sessionflow

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type hijackResponseRecorder struct{ *httptest.ResponseRecorder }

func (*hijackResponseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) { return nil, nil, nil }

func TestSpanLifecycleAndPrivacy(t *testing.T) {
	r := recorder(t, Options{Source: "lifecycle", HashKey: []byte("shared"), MaxEvents: 2})
	if span := r.Begin(); span != nil {
		t.Fatal("Begin returned a span while disabled")
	}
	if span := r.BeginRequest(httptest.NewRequest("GET", "/", nil)); span != nil {
		t.Fatal("BeginRequest returned a span while disabled")
	}
	start(t, r, "capture")
	req := httptest.NewRequest("GET", "/users/42?token=private", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "private-session"})
	span := r.BeginRequest(req)
	if span == nil {
		t.Fatal("BeginRequest returned nil while active")
	}
	obs := Observation{Method: req.Method, Route: "/users/{id}", Session: r.SessionID(req), Status: 202, Bytes: 17, Duration: 5 * time.Millisecond}
	span.End(obs)
	span.End(Observation{Method: "POST", Route: "/wrong", Session: "wrong", Status: 500})
	s := stop(t, r, "capture")
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if s.Seen != 1 || s.InFlight != 0 || len(s.Events) != 1 {
		t.Fatalf("lifecycle counters: %+v", s)
	}
	e := s.Events[0]
	if e.Method != "GET" || e.Route != "/users/{id}" || e.Status != 202 || e.Bytes != 17 || e.DurationNS != int64(5*time.Millisecond) || e.Session == "private-session" || len(e.Session) != 32 {
		t.Fatalf("observation: %+v", e)
	}
	if strings.Contains(e.Route, "42") || strings.Contains(e.Session, "private") {
		t.Fatalf("private value leaked: %+v", e)
	}
}

func TestSpanStoppedCaptureAndExcludedRequest(t *testing.T) {
	r := recorder(t, Options{Source: "lifecycle", Include: func(req *http.Request) bool { return req.URL.Path != "/excluded" }})
	start(t, r, "first")
	if got := r.BeginRequest(httptest.NewRequest("GET", "/excluded", nil)); got != nil {
		t.Fatal("excluded request admitted")
	}
	span := r.Begin()
	if span == nil {
		t.Fatal("Begin returned nil while active")
	}
	a := stop(t, r, "first")
	if a.Seen != 1 || a.InFlight != 1 {
		t.Fatalf("stopped snapshot: %+v", a)
	}
	start(t, r, "second")
	span.End(Observation{Method: "GET", Route: "/old", Session: "must-not-leak", Status: 200})
	b := stop(t, r, "second")
	if b.Seen != 0 || len(b.Events) != 0 {
		t.Fatalf("old span leaked into next capture: %+v", b)
	}
}

func TestStopDetachesEventsFromLongLivedSpan(t *testing.T) {
	r := recorder(t, Options{Source: "lifecycle", MaxEvents: 10})
	start(t, r, "capture")
	first := r.Begin()
	first.End(Observation{Method: "GET", Route: "/done", Status: 200})
	second := r.Begin()
	snapshot := stop(t, r, "capture")
	if len(snapshot.Events) != 1 || len(second.capture.snapshot.Events) != 0 {
		t.Fatalf("stopped capture retained event storage: result=%d old span=%d", len(snapshot.Events), len(second.capture.snapshot.Events))
	}
	retry, err := r.Stop("capture")
	if err != nil || len(retry.Events) != 1 || retry.Events[0].Route != "/done" {
		t.Fatalf("retained stop retry: snapshot=%+v err=%v", retry, err)
	}
}

func TestSessionAndRouteHelpers(t *testing.T) {
	r := recorder(t, Options{CookieName: "sid"})
	req := httptest.NewRequest("GET", "/items/42", nil)
	req.Pattern = "GET /items/{id}"
	req.AddCookie(&http.Cookie{Name: "sid", Value: "session-value"})
	if got := r.CookieName(); got != "sid" {
		t.Fatalf("CookieName = %q", got)
	}
	if got := r.SessionID(req); got != "session-value" {
		t.Fatalf("SessionID = %q", got)
	}
	if got := r.RoutePattern(req, ""); got != "/items/{id}" {
		t.Fatalf("default ServeMux pattern = %q", got)
	}
	if got := r.RoutePattern(req, "/fallback/{id}"); got != "/fallback/{id}" {
		t.Fatalf("fallback route = %q", got)
	}
	r = recorder(t, Options{Route: func(*http.Request) string { return "/custom" }})
	if got := r.RoutePattern(req, "/fallback"); got != "/custom" {
		t.Fatalf("custom route = %q", got)
	}
}

func TestHandlerPanicIsNotMaskedByMetadataCallbacks(t *testing.T) {
	r := recorder(t, Options{
		Source:  "panic-metadata",
		HashKey: []byte("shared"),
		Route:   func(*http.Request) string { panic("metadata route") },
		Session: func(*http.Request) string { panic("metadata session") },
	})
	start(t, r, "capture")
	h := r.WrapWithRoute(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("application") }), func(*http.Request) string { panic("metadata resolver") })
	req := request("GET", "/items/42", "raw-cookie-secret")
	req.Pattern = "GET /items/{id}"
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		h.ServeHTTP(httptest.NewRecorder(), req)
	}()
	if recovered != "application" {
		t.Fatalf("recovered panic = %v", recovered)
	}
	s := stop(t, r, "capture")
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || !s.Events[0].Panicked || s.Events[0].Status != 0 || s.Events[0].Route != "/items/{id}" || s.Events[0].Session == "raw-cookie-secret" || len(s.Events[0].Session) != 32 {
		t.Fatalf("panic event: %+v", s.Events)
	}
}

func TestSwitchingProtocolsIsFinalAndMarksBytesUnknown(t *testing.T) {
	r := recorder(t, Options{Source: "upgrade", MaxEvents: 10})
	start(t, r, "capture")
	h := r.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("lost Flusher")
		}
		if _, ok := w.(http.Hijacker); !ok {
			t.Error("lost Hijacker")
		}
		w.WriteHeader(http.StatusSwitchingProtocols)
	}))
	w := &hijackResponseRecorder{httptest.NewRecorder()}
	h.ServeHTTP(w, request("GET", "/upgrade", ""))
	if w.Code != http.StatusSwitchingProtocols {
		t.Fatalf("writer status=%d", w.Code)
	}
	s := stop(t, r, "capture")
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Status != http.StatusSwitchingProtocols || !s.Events[0].BytesUnknown || s.Events[0].Bytes != 0 || s.Events[0].DurationNS <= 0 {
		t.Fatalf("upgrade event: %+v", s.Events)
	}
}

func TestFlushCommitsImplicitOKBeforeLaterWriteHeader(t *testing.T) {
	r := recorder(t, Options{Source: "flush", MaxEvents: 10})
	start(t, r, "capture")
	h := r.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("lost Flusher")
			return
		}
		flusher.Flush()
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("body"))
	}))
	server := httptest.NewServer(h)
	defer server.Close()
	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "body" {
		t.Fatalf("wire response=%d %q", response.StatusCode, body)
	}
	s := stop(t, r, "capture")
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(s.Events) != 1 || s.Events[0].Status != http.StatusOK || s.Events[0].Bytes != 4 || s.Events[0].DurationNS <= 0 {
		t.Fatalf("flush event: %+v", s.Events)
	}
}
