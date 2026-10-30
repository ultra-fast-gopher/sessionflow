package sessionflow

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func recorder(t testing.TB, opts Options) *Recorder {
	t.Helper()
	r, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func start(t testing.TB, r *Recorder, id string) {
	t.Helper()
	if err := r.Start(id); err != nil {
		t.Fatal(err)
	}
}
func stop(t testing.TB, r *Recorder, id string) Snapshot {
	t.Helper()
	s, err := r.Stop(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func request(method, path, session string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: "session", Value: session})
	}
	return req
}

func TestMiddlewarePrivacyAndHTTPBehavior(t *testing.T) {
	r := recorder(t, Options{MaxEvents: 8, HashKey: []byte("shared")})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, req *http.Request) {
		if _, ok := w.(http.Flusher); !ok {
			t.Error("lost Flusher")
		}
		w.WriteHeader(103)
		w.WriteHeader(201)
		_, _ = w.Write([]byte("hello"))
	})
	h := r.Wrap(mux)
	server := httptest.NewServer(h)
	defer server.Close()
	start(t, r, "test")
	req, err := http.NewRequest(http.MethodGet, server.URL+"/items/42?token=secret", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: "session", Value: "raw-secret-cookie"})
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusCreated || string(body) != "hello" {
		t.Fatalf("wire response: status=%d body=%q", response.StatusCode, body)
	}
	s := stop(t, r, "test")
	if len(s.Events) != 1 {
		t.Fatalf("events: %+v", s)
	}
	e := s.Events[0]
	if e.Route != "/items/{id}" || e.Status != 201 || e.Bytes != 5 || e.Session == "" || e.Panicked {
		t.Fatalf("event: %+v", e)
	}
	data, _ := json.Marshal(s)
	if strings.Contains(string(data), "secret") || strings.Contains(string(data), "/items/42") {
		t.Fatalf("raw data leaked: %s", data)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCapacityAndNoAnonymousLinkage(t *testing.T) {
	r := recorder(t, Options{MaxEvents: 2})
	start(t, r, "test")
	h := r.Wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
	for i := 0; i < 5; i++ {
		h.ServeHTTP(httptest.NewRecorder(), request("GET", "/unknown", ""))
	}
	h.ServeHTTP(httptest.NewRecorder(), request("GET", "/debug/sessionflow/status", ""))
	s := stop(t, r, "test")
	if len(s.Events) != 2 || s.Dropped != 3 || s.Seen != 5 || s.Unidentified != 5 || s.InFlight != 0 {
		t.Fatalf("counters: %+v", s)
	}
	if s.Events[0].Route != "<unmatched>" || s.Events[0].Session != "" {
		t.Fatal(s.Events[0])
	}
}

func TestWholeSessionSamplingAcrossSources(t *testing.T) {
	var snapshots []Snapshot
	for _, source := range []string{"a", "b"} {
		r := recorder(t, Options{Source: source, HashKey: []byte("same-key"), SampleEvery: 3, MaxEvents: 1000})
		start(t, r, "test")
		h := r.Wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
		for i := 0; i < 100; i++ {
			for j := 0; j < 3; j++ {
				h.ServeHTTP(httptest.NewRecorder(), request("GET", "/", fmt.Sprint(i)))
			}
		}
		snapshots = append(snapshots, stop(t, r, "test"))
	}
	a, b := snapshots[0], snapshots[1]
	if a.SampledOut == 0 || len(a.Events) == 0 || len(a.Events) != len(b.Events) {
		t.Fatalf("sampling: %+v", a)
	}
	counts := map[string]int{}
	for i, e := range a.Events {
		counts[e.Session]++
		if e.Session != b.Events[i].Session {
			t.Fatal("inconsistent session hashing")
		}
	}
	for _, count := range counts {
		if count != 3 {
			t.Fatal("partial session sampling")
		}
	}
}

func TestStopInFlightCannotLeakIntoNextCapture(t *testing.T) {
	r := recorder(t, Options{MaxEvents: 10})
	entered, release := make(chan struct{}), make(chan struct{})
	h := r.Wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { close(entered); <-release }))
	start(t, r, "a")
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); h.ServeHTTP(httptest.NewRecorder(), request("GET", "/", "s")) }()
	<-entered
	a := stop(t, r, "a")
	if a.InFlight != 1 || len(a.Events) != 0 {
		t.Fatalf("snapshot: %+v", a)
	}
	start(t, r, "b")
	close(release)
	wg.Wait()
	b := stop(t, r, "b")
	if b.Seen != 0 || len(b.Events) != 0 {
		t.Fatalf("leaked event: %+v", b)
	}
}

func TestPanicStillPropagates(t *testing.T) {
	r := recorder(t, Options{MaxEvents: 10})
	start(t, r, "test")
	h := r.Wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { panic("expected") }))
	func() {
		defer func() {
			if recover() != "expected" {
				t.Error("panic was swallowed")
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), request("GET", "/", "s"))
	}()
	s := stop(t, r, "test")
	if len(s.Events) != 1 || !s.Events[0].Panicked || s.Events[0].Status != 0 {
		t.Fatal(s)
	}
}

func TestConcurrentRequestsAndControls(t *testing.T) {
	r := recorder(t, Options{MaxEvents: 2000})
	h := r.Wrap(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {}))
	start(t, r, "test")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				h.ServeHTTP(httptest.NewRecorder(), request("GET", "/", "s"))
				_ = r.Status()
			}
		}()
	}
	wg.Wait()
	s := stop(t, r, "test")
	if s.Seen != 2000 || len(s.Events) != 2000 || s.Dropped != 0 {
		t.Fatal("events lost")
	}
	sequences := map[uint64]bool{}
	for _, e := range s.Events {
		if sequences[e.Sequence] {
			t.Fatal("duplicate sequence")
		}
		sequences[e.Sequence] = true
	}
}

func TestControlAuthenticationAndConflicts(t *testing.T) {
	r := recorder(t, Options{MaxEvents: 10})
	h := r.ControlHandler("test-token")
	call := func(path, body, token string) int {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w.Code
	}
	if got := call("/start", `{"capture_id":"a"}`, ""); got != 401 {
		t.Fatal(got)
	}
	for _, id := range []string{"a", "a"} {
		if got := call("/start", `{"capture_id":"`+id+`"}`, "Bearer test-token"); got != 202 {
			t.Fatal(got)
		}
	}
	if got := call("/start", `{"capture_id":"b"}`, "Bearer test-token"); got != 409 {
		t.Fatal(got)
	}
	if got := call("/stop", `{"capture_id":"b"}`, "Bearer test-token"); got != 404 {
		t.Fatal(got)
	}
	if got := call("/stop/", `{"capture_id":"a"}`, "Bearer test-token"); got != 200 {
		t.Fatal(got)
	}
	if got := call("/stop", `{"capture_id":"a"}`, "Bearer test-token"); got != 200 {
		t.Fatal("stop retry failed", got)
	}
	if got := call("/start", `{"capture_id":"new"}`, "Bearer test-token"); got != 202 {
		t.Fatal(got)
	}
	if got := call("/stop", `{"capture_id":"a"}`, "Bearer test-token"); got != 404 {
		t.Fatal("old capture retained after start", got)
	}
	if got := call("/start", `{"capture_id":"a"} {}`, "Bearer test-token"); got != 400 {
		t.Fatal(got)
	}
}

type discardWriter struct{}

func (discardWriter) Header() http.Header         { return nil }
func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriter) WriteHeader(int)             {}

func BenchmarkMiddleware(b *testing.B) {
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	for _, mode := range []string{"baseline", "disabled", "recording", "full", "sampled"} {
		b.Run(mode, func(b *testing.B) {
			opts := Options{MaxEvents: 10000, HashKey: []byte("benchmark-key"), Route: func(*http.Request) string { return "/items/{id}" }}
			if mode == "sampled" {
				opts.SampleEvery = 1000000
			}
			r := recorder(b, opts)
			var h http.Handler = base
			if mode != "baseline" {
				h = r.Wrap(base)
			}
			if mode == "recording" || mode == "full" || mode == "sampled" {
				start(b, r, "bench")
			}
			req := request("GET", "/items/42", "bench-session")
			w := discardWriter{}
			if mode == "full" {
				for i := 0; i < 10000; i++ {
					h.ServeHTTP(w, req)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if mode == "recording" && i > 0 && i%10000 == 0 {
					b.StopTimer()
					stop(b, r, "bench")
					start(b, r, "bench")
					b.StartTimer()
				}
				h.ServeHTTP(w, req)
			}
		})
	}
}
