// Package sessionflow records bounded, explicitly enabled HTTP request captures.
package sessionflow

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/felixge/httpsnoop"
)

const Version = 1

type Event struct {
	Sequence     uint64 `json:"sequence"`
	Session      string `json:"session,omitempty"`
	Method       string `json:"method"`
	Route        string `json:"route"`
	StartNS      int64  `json:"start_ns"`
	DurationNS   int64  `json:"duration_ns"`
	Status       int    `json:"status"`
	Bytes        int64  `json:"bytes"`
	BytesUnknown bool   `json:"bytes_unknown,omitempty"`
	Panicked     bool   `json:"panicked,omitempty"`
}

type Snapshot struct {
	Version      int       `json:"version"`
	CaptureID    string    `json:"capture_id"`
	Source       string    `json:"source"`
	KeyID        string    `json:"key_id"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at"`
	SampleEvery  uint64    `json:"sample_every"`
	Capacity     int       `json:"capacity"`
	Seen         uint64    `json:"seen"`
	SampledOut   uint64    `json:"sampled_out"`
	Dropped      uint64    `json:"dropped"`
	Unidentified uint64    `json:"unidentified"`
	InFlight     uint64    `json:"in_flight"`
	Events       []Event   `json:"events"`
}

type Bundle struct {
	Version  int        `json:"version"`
	Captures []Snapshot `json:"captures"`
	Errors   []string   `json:"errors,omitempty"`
}

type Options struct {
	// Source must identify a process uniquely within a collection.
	Source string
	// CookieName defaults to "session". Session overrides cookie extraction and
	// runs after the handler, so authenticated user IDs in context can be used.
	CookieName string
	Session    func(*http.Request) string
	// Route runs after the handler. Default: Request.Pattern, otherwise a single
	// <unmatched> bucket. Raw URLs, query strings and cookie values are never saved.
	Route func(*http.Request) string
	// Include runs before instrumentation; use it to exclude assets and health checks.
	Include func(*http.Request) bool
	// HashKey must match across servers to join sessions. Empty generates a random
	// per-recorder key. Keys are copied and never included in exported data.
	HashKey   []byte
	MaxEvents int
	// SampleEvery keeps approximately 1/N whole sessions, not isolated requests.
	SampleEvery uint64
}

type capture struct {
	snapshot Snapshot
	finished uint64
	sequence uint64
}

type Recorder struct {
	opts   Options
	keyID  string
	active atomic.Pointer[capture]
	mu     sync.Mutex
	macs   sync.Pool
	last   *Snapshot
}

type sessionMAC struct {
	mac hash.Hash
	sum [sha256.Size]byte
}

// Observation contains the request details collected by an adapter after the
// framework has resolved its route and the response has completed.
type Observation struct {
	Method       string
	Route        string
	Session      string
	Status       int
	Bytes        int64
	BytesUnknown bool
	Panicked     bool
	Duration     time.Duration
}

// Span represents one request admitted to an active capture. A nil Span means
// recording was disabled or the request was excluded when it began.
type Span struct {
	recorder *Recorder
	capture  *capture
	sequence uint64
	start    time.Time
	ended    atomic.Bool
}

func New(opts Options) (*Recorder, error) {
	if opts.MaxEvents == 0 {
		opts.MaxEvents = 100000
	}
	if opts.MaxEvents < 1 {
		return nil, errors.New("MaxEvents must be positive")
	}
	if opts.SampleEvery == 0 {
		opts.SampleEvery = 1
	}
	if opts.CookieName == "" {
		opts.CookieName = "session"
	}
	if opts.Include == nil {
		opts.Include = DefaultInclude
	}
	if opts.Source == "" {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return nil, err
		}
		opts.Source = hex.EncodeToString(b[:])
	}
	key := append([]byte(nil), opts.HashKey...)
	opts.HashKey = nil
	if len(key) == 0 {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
	}
	id := sha256.Sum256(key)
	r := &Recorder{opts: opts, keyID: hex.EncodeToString(id[:8])}
	r.macs.New = func() any { return &sessionMAC{mac: hmac.New(sha256.New, key)} }
	return r, nil
}

// Start enables a new capture. Repeating the active ID is idempotent.
func (r *Recorder) Start(id string) error {
	if id == "" || len(id) > 128 {
		return errors.New("capture id must contain 1..128 bytes")
	}
	// Allocate outside the hot-path mutex. Call the control endpoint off-benchmark.
	c := &capture{snapshot: Snapshot{Version: Version, CaptureID: id,
		Source: r.opts.Source, KeyID: r.keyID, StartedAt: time.Now().UTC(),
		Capacity: r.opts.MaxEvents, SampleEvery: r.opts.SampleEvery,
		Events: make([]Event, 0, r.opts.MaxEvents)}}
	r.mu.Lock()
	defer r.mu.Unlock()
	if current := r.active.Load(); current != nil {
		if current.snapshot.CaptureID == id {
			return nil
		}
		return errors.New("a different capture is already active")
	}
	r.last = nil
	r.active.Store(c)
	return nil
}

// Stop atomically detaches completed records. Requests that are still running
// are counted in InFlight and cannot leak into a subsequent capture.
// The last stopped result is retained for retries until the next Start.
// Treat returned snapshots and their Events as immutable.
func (r *Recorder) Stop(id string) (Snapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.active.Load()
	if c == nil && r.last != nil && r.last.CaptureID == id {
		return *r.last, nil
	}
	if c == nil || c.snapshot.CaptureID != id {
		return Snapshot{}, errors.New("capture not found")
	}
	r.active.Store(nil)
	c.snapshot.EndedAt = time.Now().UTC()
	c.snapshot.InFlight = c.snapshot.Seen - c.finished
	// Detach the result so spans that remain in flight after Stop cannot keep
	// the completed event backing array alive across later captures.
	detached := c.snapshot
	r.last = &detached
	c.snapshot.Events = nil
	return detached, nil
}

type Status struct {
	Active            bool   `json:"active"`
	CaptureID         string `json:"capture_id,omitempty"`
	Source            string `json:"source"`
	Recorded          int    `json:"recorded"`
	Seen              uint64 `json:"seen"`
	Dropped           uint64 `json:"dropped"`
	Capacity          int    `json:"capacity"`
	RetainedCaptureID string `json:"retained_capture_id,omitempty"`
}

func (r *Recorder) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := Status{Source: r.opts.Source, Capacity: r.opts.MaxEvents}
	if r.last != nil {
		s.RetainedCaptureID = r.last.CaptureID
	}
	if c := r.active.Load(); c != nil {
		s.Active, s.CaptureID, s.Recorded, s.Seen, s.Dropped = true, c.snapshot.CaptureID, len(c.snapshot.Events), c.snapshot.Seen, c.snapshot.Dropped
	}
	return s
}

// Wrap preserves optional ResponseWriter interfaces through httpsnoop.
// Install outside the router; router-specific Route callbacks resolve templates.
func (r *Recorder) Wrap(next http.Handler) http.Handler {
	return r.WrapWithRoute(next, nil)
}

// WrapWithRoute preserves optional ResponseWriter interfaces through httpsnoop.
// The resolver runs after the handler and supplies a route fallback; Options.Route
// remains authoritative when configured.
func (r *Recorder) WrapWithRoute(next http.Handler, resolver func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		span := r.BeginRequest(req)
		if span == nil {
			next.ServeHTTP(w, req)
			return
		}
		start := span.start
		metrics := httpsnoop.Metrics{Code: http.StatusOK}
		completed := false
		defer func() {
			// A panic continues unwinding; we do not recover or change application behavior.
			if !completed {
				metrics.Duration = time.Since(start)
			}
			route := ""
			session := ""
			if completed {
				if resolver != nil {
					route = resolver(req)
				}
				route = r.RoutePattern(req, route)
				session = r.SessionID(req)
			} else {
				// Metadata callbacks can panic too (for example, when they assume
				// handler-populated context). Do not let them replace the original
				// application panic while the deferred span closes.
				route = requestPattern(req)
				session = r.cookieSession(req)
			}
			span.End(Observation{Method: req.Method, Route: route,
				Session: session, Status: metrics.Code, Bytes: metrics.Written,
				Panicked: !completed, Duration: metrics.Duration})
		}()
		metrics = captureHTTPMetrics(next, w, req)
		completed = true
	})
}

// captureHTTPMetrics follows net/http's response-code semantics while using
// httpsnoop.Wrap to retain every optional ResponseWriter interface. A 101 is
// final, unlike other 1xx responses; they remain informational until a final
// status or implicit 200 is written.
func captureHTTPMetrics(next http.Handler, w http.ResponseWriter, req *http.Request) (metrics httpsnoop.Metrics) {
	metrics.Code = http.StatusOK
	start := time.Now()
	finalStatusWritten := false
	hooks := httpsnoop.Hooks{
		WriteHeader: func(next httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
			return func(code int) {
				next(code)
				if code == http.StatusSwitchingProtocols {
					if !finalStatusWritten {
						metrics.Code = code
						finalStatusWritten = true
					}
				} else if (code < 100 || code > 199) && !finalStatusWritten {
					metrics.Code = code
					finalStatusWritten = true
				}
			}
		},
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(p []byte) (int, error) {
				n, err := next(p)
				metrics.Written += int64(n)
				if !finalStatusWritten {
					metrics.Code = http.StatusOK
					finalStatusWritten = true
				}
				return n, err
			}
		},
		ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(src io.Reader) (int64, error) {
				n, err := next(src)
				metrics.Written += n
				if !finalStatusWritten {
					metrics.Code = http.StatusOK
					finalStatusWritten = true
				}
				return n, err
			}
		},
		Flush: func(next httpsnoop.FlushFunc) httpsnoop.FlushFunc {
			return func() {
				next()
				if !finalStatusWritten {
					metrics.Code = http.StatusOK
					finalStatusWritten = true
				}
			}
		},
	}
	next.ServeHTTP(httpsnoop.Wrap(w, hooks), req)
	metrics.Duration = time.Since(start)
	return metrics
}

// Begin opens a request span when a capture is active. It has no request to
// evaluate, so it does not apply Options.Include; use BeginRequest for HTTP work.
func (r *Recorder) Begin() *Span { return r.begin(nil) }

// Enabled reports whether a capture is currently active. Native adapters can
// use it as a cheap preflight before converting framework requests; Begin and
// BeginRequest recheck the active capture before admission.
func (r *Recorder) Enabled() bool { return r.active.Load() != nil }

// BeginRequest opens a request span when a capture is active and Include accepts
// the request.
func (r *Recorder) BeginRequest(req *http.Request) *Span { return r.begin(req) }

func (r *Recorder) begin(req *http.Request) *Span {
	c := r.active.Load()
	if c == nil || (req != nil && r.opts.Include != nil && !r.opts.Include(req)) {
		return nil
	}
	r.mu.Lock()
	if r.active.Load() != c {
		r.mu.Unlock()
		return nil
	}
	c.snapshot.Seen++
	c.sequence++
	sequence := c.sequence
	r.mu.Unlock()
	return &Span{recorder: r, capture: c, sequence: sequence, start: time.Now()}
}

// End records the completed observation once. Raw session identifiers are
// immediately reduced to a keyed hash and never enter the capture.
func (s *Span) End(obs Observation) {
	if s == nil || !s.ended.CompareAndSwap(false, true) {
		return
	}
	duration := obs.Duration
	if duration <= 0 {
		duration = time.Since(s.start)
	}
	if duration < 0 {
		duration = 0
	}
	r, c := s.recorder, s.capture
	// Copy adapter-provided strings before they can be reused by a framework.
	method, route, session := strings.Clone(obs.Method), strings.Clone(obs.Route), obs.Session
	if r.active.Load() != c {
		return
	}
	if method == "" {
		method = "UNKNOWN"
	}
	if route == "" {
		route = "<unmatched>"
	}
	keep := true
	if session != "" {
		state := r.macs.Get().(*sessionMAC)
		state.mac.Reset()
		_, _ = state.mac.Write([]byte(session))
		sum := state.mac.Sum(state.sum[:0])
		var sample uint64
		for _, b := range sum[:8] {
			sample = sample<<8 | uint64(b)
		}
		keep = sample%r.opts.SampleEvery == 0
		var encoded [32]byte
		hex.Encode(encoded[:], sum[:16])
		session = string(encoded[:])
		r.macs.Put(state)
	}
	r.mu.Lock()
	if r.active.Load() != c {
		r.mu.Unlock()
		return
	}
	c.finished++
	if !keep {
		c.snapshot.SampledOut++
		r.mu.Unlock()
		return
	}
	if session == "" {
		c.snapshot.Unidentified++
	}
	if len(c.snapshot.Events) == cap(c.snapshot.Events) {
		c.snapshot.Dropped++
		r.mu.Unlock()
		return
	}
	status := obs.Status
	if obs.Panicked {
		status = 0
	}
	c.snapshot.Events = append(c.snapshot.Events, Event{Sequence: s.sequence, Session: session,
		Method: method, Route: route, StartNS: s.start.UnixNano(), DurationNS: duration.Nanoseconds(),
		Status: status, Bytes: obs.Bytes, BytesUnknown: obs.BytesUnknown || status == http.StatusSwitchingProtocols, Panicked: obs.Panicked})
	r.mu.Unlock()
}

// SessionID returns the raw session identifier for immediate submission to Span.End.
func (r *Recorder) SessionID(req *http.Request) string {
	if req == nil {
		return ""
	}
	if r.opts.Session != nil {
		return r.opts.Session(req)
	}
	return r.cookieSession(req)
}

func (r *Recorder) cookieSession(req *http.Request) string {
	if req == nil {
		return ""
	}
	if cookie, err := req.Cookie(r.opts.CookieName); err == nil {
		return cookie.Value
	}
	return ""
}

// CookieName returns the configured cookie name, after applying its default.
func (r *Recorder) CookieName() string { return r.opts.CookieName }

// RoutePattern resolves an application route without persisting a concrete URL.
func (r *Recorder) RoutePattern(req *http.Request, fallback string) string {
	if r.opts.Route != nil {
		return r.opts.Route(req)
	}
	if fallback != "" {
		return fallback
	}
	if req == nil {
		return ""
	}
	return requestPattern(req)
}

func requestPattern(req *http.Request) string {
	if req == nil {
		return ""
	}
	route := req.Pattern
	if _, path, ok := strings.Cut(route, " "); ok {
		return path
	}
	return route
}

// DefaultInclude excludes this tool's control routes and common profiling routes.
// It can be composed with an application's own asset filtering.
func DefaultInclude(r *http.Request) bool {
	return !strings.HasPrefix(r.URL.Path, "/debug/")
}

// Validate checks data before analysis. Captures from other schema versions are
// rejected instead of being silently misinterpreted.
func (s Snapshot) Validate() error {
	if s.Version != Version {
		return fmt.Errorf("unsupported capture version %d", s.Version)
	}
	if s.CaptureID == "" || s.Source == "" || s.KeyID == "" {
		return errors.New("capture_id, source, and key_id are required")
	}
	if s.SampleEvery == 0 || s.Capacity < len(s.Events) {
		return errors.New("invalid sampling or capacity")
	}
	if s.StartedAt.IsZero() || s.EndedAt.Before(s.StartedAt) {
		return errors.New("invalid capture time range")
	}
	for _, e := range s.Events {
		if e.Method == "" || e.Route == "" || e.StartNS <= 0 || e.DurationNS < 0 || e.Sequence == 0 || e.Status < 0 || e.Status > 599 || e.Bytes < 0 {
			return errors.New("invalid event")
		}
		if e.StartNS > int64(^uint64(0)>>1)-e.DurationNS {
			return errors.New("event time overflow")
		}
	}
	return nil
}
