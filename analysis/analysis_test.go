package analysis

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	flow "github.com/ultra-fast-gopher/sessionflow"
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func event(seq uint64, session, route string, start, duration time.Duration, status int) flow.Event {
	return flow.Event{Sequence: seq, Session: session, Method: "GET", Route: route, StartNS: epoch.Add(start).UnixNano(), DurationNS: int64(duration), Status: status}
}
func snapshot(source string, events ...flow.Event) flow.Snapshot {
	return flow.Snapshot{Version: 1, CaptureID: "run", Source: source, KeyID: "key", StartedAt: epoch, EndedAt: epoch.Add(time.Hour), SampleEvery: 1, Capacity: 100, Seen: uint64(len(events)), Events: events}
}

func TestJoinSourcesSortStartAndNeverJoinAnonymous(t *testing.T) {
	a := snapshot("a", event(2, "s", "/done", 30*time.Millisecond, time.Millisecond, 500), event(1, "", "/anonymous-a", time.Millisecond, time.Millisecond, 200))
	b := snapshot("b", event(1, "s", "/start", 0, 10*time.Millisecond, 200), event(2, "", "/anonymous-b", 20*time.Millisecond, time.Millisecond, 200))
	r, err := Analyze([]flow.Snapshot{a, b}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.Sessions != 1 || r.Summary.Transitions != 1 || r.Summary.Events != 4 {
		t.Fatalf("summary: %+v", r.Summary)
	}
	e := r.Edges[0]
	if e.From != "GET /start" || e.To != "GET /done" || e.Errors != 1 || e.P95MS != 1 || e.MeanGapMS != 20 || e.Probability != 1 {
		t.Fatalf("edge: %+v", e)
	}
	if r.Sessions[0].Requests[0].Source != "b" {
		t.Fatal("completion order was used")
	}
}

func TestOverlappingIntervalsAndIdleGaps(t *testing.T) {
	c := snapshot("a",
		event(1, "s", "/long", 0, 100*time.Millisecond, 200),
		event(2, "s", "/short", 10*time.Millisecond, time.Millisecond, 200),
		event(3, "s", "/nested", 20*time.Millisecond, time.Millisecond, 200),
		event(4, "s", "/after", 110*time.Millisecond, time.Millisecond, 200),
		event(5, "s", "/new-episode", time.Minute, time.Millisecond, 200))
	r, err := Analyze([]flow.Snapshot{c}, Options{MaxGap: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.Overlaps != 2 || r.Summary.GapBreaks != 1 || r.Summary.Transitions != 1 {
		t.Fatal(r.Summary)
	}
	if r.Edges[0].From != "GET /nested" || r.Edges[0].To != "GET /after" {
		t.Fatal(r.Edges)
	}
	// The long request remains the frontier even after shorter requests complete.
	if r.Edges[0].MeanGapMS != 10 {
		t.Fatal(r.Edges)
	}
}

func TestDifferentRunsAndHashIDsCannotBeJoined(t *testing.T) {
	a := snapshot("a", event(1, "same", "/a", 0, time.Millisecond, 200))
	b := snapshot("b", event(1, "same", "/b", 2*time.Millisecond, time.Millisecond, 200))
	b.KeyID = "other"
	c := snapshot("c", event(1, "same", "/c", 4*time.Millisecond, time.Millisecond, 200))
	c.CaptureID = "other-run"
	r, err := Analyze([]flow.Snapshot{a, b, c}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary.Sessions != 3 || r.Summary.Transitions != 0 || len(r.Summary.Warnings) != 1 {
		t.Fatal(r.Summary)
	}
	if _, err := Analyze([]flow.Snapshot{a, a}, Options{}); err == nil {
		t.Fatal("accepted duplicate capture")
	}
}

func TestUnknownBytesCountedWithoutInventingTotals(t *testing.T) {
	e := event(1, "session", "/stream", 0, time.Millisecond, 200)
	e.BytesUnknown = true
	r, err := Analyze([]flow.Snapshot{snapshot("stream", e)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Nodes) != 1 || r.Nodes[0].Bytes != 0 || r.Nodes[0].BytesUnknown != 1 {
		t.Fatalf("node byte metrics: %+v", r.Nodes)
	}
}

func TestProbabilitiesFiltersAndStableJSON(t *testing.T) {
	c := snapshot("a", event(1, "a", "/start", 0, time.Millisecond, 200), event(2, "a", "/one", 2*time.Millisecond, time.Millisecond, 200), event(3, "b", "/start", 0, time.Millisecond, 200), event(4, "b", "/two", 2*time.Millisecond, time.Millisecond, 200))
	r, err := Analyze([]flow.Snapshot{c}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range r.Edges {
		if e.Probability != .5 || e.Sessions != 1 {
			t.Fatal(e)
		}
	}
	first, _ := json.Marshal(r)
	for i := 0; i < 10; i++ {
		next, _ := Analyze([]flow.Snapshot{c}, Options{})
		data, _ := json.Marshal(next)
		if string(data) != string(first) {
			t.Fatal("nondeterministic output")
		}
	}
	filtered, err := Analyze([]flow.Snapshot{c}, Options{Session: "a"})
	if err != nil || filtered.Summary.Sessions != 1 || len(filtered.Edges) != 1 {
		t.Fatal(filtered, err)
	}
	if _, err := Analyze([]flow.Snapshot{c}, Options{MaxGap: -1}); err == nil {
		t.Fatal("negative gap accepted")
	}
}

func TestLossAndSamplingWarnings(t *testing.T) {
	c := snapshot("a", event(1, "s", "/", 0, time.Millisecond, 200))
	c.Dropped = 3
	c.Unidentified = 2
	c.InFlight = 1
	c.SampledOut = 4
	r, err := Analyze([]flow.Snapshot{c}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Summary.Warnings) != 4 {
		t.Fatal(r.Summary)
	}
	c.Version = 999
	if _, err := Analyze([]flow.Snapshot{c}, Options{}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatal(err)
	}
}
