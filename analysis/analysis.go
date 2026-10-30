// Package analysis reconstructs observed request sequences, not browser causality.
package analysis

import (
	"fmt"
	"sort"
	"strings"
	"time"

	flow "github.com/ultra-fast-gopher/sessionflow"
)

type Options struct {
	MaxGap  time.Duration
	Session string
}

type Summary struct {
	Captures     int      `json:"captures"`
	Events       int      `json:"events"`
	Sessions     int      `json:"sessions"`
	Transitions  int      `json:"transitions"`
	Overlaps     int      `json:"overlaps"`
	GapBreaks    int      `json:"gap_breaks"`
	Seen         uint64   `json:"seen"`
	Dropped      uint64   `json:"dropped"`
	SampledOut   uint64   `json:"sampled_out"`
	Unidentified uint64   `json:"unidentified"`
	InFlight     uint64   `json:"in_flight"`
	MaxGapMS     float64  `json:"max_gap_ms"`
	Warnings     []string `json:"warnings"`
}

type Node struct {
	ID           string  `json:"id"`
	Count        int     `json:"count"`
	Sessions     int     `json:"sessions"`
	Errors       int     `json:"errors"`
	TotalMS      float64 `json:"total_ms"`
	P50MS        float64 `json:"p50_ms"`
	P95MS        float64 `json:"p95_ms"`
	Bytes        int64   `json:"bytes"`
	BytesUnknown int     `json:"bytes_unknown"`
}

type Edge struct {
	From        string  `json:"from"`
	To          string  `json:"to"`
	Count       int     `json:"count"`
	Sessions    int     `json:"sessions"`
	Probability float64 `json:"probability"`
	Errors      int     `json:"errors"`
	TotalMS     float64 `json:"total_ms"`
	P95MS       float64 `json:"p95_ms"`
	MeanGapMS   float64 `json:"mean_gap_ms"`
}

type Request struct {
	flow.Event
	Source   string `json:"source"`
	Node     string `json:"node"`
	Relation string `json:"relation"` // start, next, overlap, gap
}

type Session struct {
	ID        string    `json:"id"`
	Hash      string    `json:"hash"`
	CaptureID string    `json:"capture_id"`
	KeyID     string    `json:"key_id"`
	Requests  []Request `json:"requests"`
}

type Path struct {
	Nodes []string `json:"nodes"`
	Count int      `json:"count"`
}

type Report struct {
	Version  int       `json:"version"`
	Summary  Summary   `json:"summary"`
	Nodes    []Node    `json:"nodes"`
	Edges    []Edge    `json:"edges"`
	Paths    []Path    `json:"paths"`
	Sessions []Session `json:"sessions"`
}

type accumulator struct {
	durations []int64
	sessions  map[string]bool
}

func percentile(values []int64, q float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	i := int(float64(len(values)-1)*q + 0.5)
	return float64(values[i]) / 1e6
}

func Analyze(captures []flow.Snapshot, opts Options) (*Report, error) {
	if opts.MaxGap == 0 {
		opts.MaxGap = 30 * time.Minute
	}
	if opts.MaxGap < 0 {
		return nil, fmt.Errorf("max gap must be positive")
	}
	r := &Report{Version: flow.Version, Nodes: []Node{}, Edges: []Edge{}, Paths: []Path{}, Sessions: []Session{}}
	r.Summary = Summary{Captures: len(captures), MaxGapMS: float64(opts.MaxGap) / 1e6, Warnings: []string{}}
	sessions := map[string]*Session{}
	nodes := map[string]*Node{}
	nodeAcc := map[string]*accumulator{}
	edges := map[[2]string]*Edge{}
	edgeAcc := map[[2]string]*accumulator{}
	paths := map[string]*Path{}
	hashIDs := map[string]bool{}
	samples := map[uint64]bool{}
	seenCaptures := map[[2]string]bool{}
	addNode := func(e flow.Event, sid string) string {
		id := e.Method + " " + e.Route
		n := nodes[id]
		if n == nil {
			n = &Node{ID: id}
			nodes[id] = n
			nodeAcc[id] = &accumulator{sessions: map[string]bool{}}
		}
		n.Count++
		n.TotalMS += float64(e.DurationNS) / 1e6
		n.Bytes += e.Bytes
		if e.BytesUnknown {
			n.BytesUnknown++
		}
		if e.Status >= 400 || e.Panicked {
			n.Errors++
		}
		a := nodeAcc[id]
		a.durations = append(a.durations, e.DurationNS)
		if sid != "" {
			a.sessions[sid] = true
		}
		return id
	}
	for _, c := range captures {
		if err := c.Validate(); err != nil {
			return nil, fmt.Errorf("%s/%s: %w", c.CaptureID, c.Source, err)
		}
		identity := [2]string{c.CaptureID, c.Source}
		if seenCaptures[identity] {
			return nil, fmt.Errorf("duplicate capture %s from %s", c.CaptureID, c.Source)
		}
		seenCaptures[identity] = true
		hashIDs[c.KeyID] = true
		samples[c.SampleEvery] = true
		r.Summary.Seen += c.Seen
		r.Summary.Dropped += c.Dropped
		r.Summary.SampledOut += c.SampledOut
		r.Summary.Unidentified += c.Unidentified
		r.Summary.InFlight += c.InFlight
		for _, e := range c.Events {
			sid := ""
			if e.Session != "" {
				sid = c.CaptureID + ":" + c.KeyID + ":" + e.Session
			}
			if opts.Session != "" && e.Session != opts.Session && sid != opts.Session {
				continue
			}
			r.Summary.Events++
			node := addNode(e, sid)
			if sid == "" {
				continue
			}
			s := sessions[sid]
			if s == nil {
				s = &Session{ID: sid, Hash: e.Session, CaptureID: c.CaptureID, KeyID: c.KeyID}
				sessions[sid] = s
			}
			s.Requests = append(s.Requests, Request{Event: e, Source: c.Source, Node: node})
		}
	}
	for _, s := range sessions {
		sort.Slice(s.Requests, func(i, j int) bool {
			a, b := s.Requests[i], s.Requests[j]
			if a.StartNS != b.StartNS {
				return a.StartNS < b.StartNS
			}
			if a.Source != b.Source {
				return a.Source < b.Source
			}
			return a.Sequence < b.Sequence
		})
		var path []string
		flush := func() {
			if len(path) == 0 {
				return
			}
			// NUL is escaped by JSON and avoids ambiguous joins of route names.
			key := strings.Join(path, "\x00")
			if p := paths[key]; p != nil {
				p.Count++
			} else {
				paths[key] = &Path{Nodes: append([]string(nil), path...), Count: 1}
			}
			path = nil
		}
		var frontier int64
		for i := range s.Requests {
			curr := &s.Requests[i]
			end := curr.StartNS + curr.DurationNS
			curr.Relation = "start"
			if i > 0 {
				prev := s.Requests[i-1]
				gap := curr.StartNS - frontier
				switch {
				case curr.StartNS < frontier || curr.StartNS == prev.StartNS:
					curr.Relation = "overlap"
					r.Summary.Overlaps++
					flush()
				case gap > int64(opts.MaxGap):
					curr.Relation = "gap"
					r.Summary.GapBreaks++
					flush()
				default:
					curr.Relation = "next"
					r.Summary.Transitions++
					key := [2]string{prev.Node, curr.Node}
					e := edges[key]
					if e == nil {
						e = &Edge{From: prev.Node, To: curr.Node}
						edges[key] = e
						edgeAcc[key] = &accumulator{sessions: map[string]bool{}}
					}
					e.Count++
					e.TotalMS += float64(curr.DurationNS) / 1e6
					e.MeanGapMS += float64(gap) / 1e6
					if curr.Status >= 400 || curr.Panicked {
						e.Errors++
					}
					a := edgeAcc[key]
					a.sessions[s.ID] = true
					a.durations = append(a.durations, curr.DurationNS)
				}
			}
			path = append(path, curr.Node)
			if end > frontier {
				frontier = end
			}
		}
		flush()
		r.Sessions = append(r.Sessions, *s)
	}
	r.Summary.Sessions = len(r.Sessions)
	if len(hashIDs) > 1 {
		r.Summary.Warnings = append(r.Summary.Warnings, "Different session hash IDs: sessions across those sources cannot be joined.")
	}
	if len(samples) > 1 {
		r.Summary.Warnings = append(r.Summary.Warnings, "Different sampling rates: cross-source sequences may be incomplete.")
	}
	if r.Summary.Dropped > 0 {
		r.Summary.Warnings = append(r.Summary.Warnings, "Capacity exceeded: request sequences and transition counts are incomplete.")
	}
	if r.Summary.Unidentified > 0 {
		r.Summary.Warnings = append(r.Summary.Warnings, "Requests without a session appear in node metrics only.")
	}
	if r.Summary.InFlight > 0 {
		r.Summary.Warnings = append(r.Summary.Warnings, "Requests still running at stop were excluded.")
	}
	if r.Summary.SampledOut > 0 {
		r.Summary.Warnings = append(r.Summary.Warnings, "Whole-session sampling is enabled; counts are not extrapolated.")
	}
	if r.Summary.Overlaps > 0 {
		r.Summary.Warnings = append(r.Summary.Warnings, "Overlapping requests are marked in timelines and excluded from transitions.")
	}
	for id, n := range nodes {
		a := nodeAcc[id]
		n.Sessions = len(a.sessions)
		n.P50MS = percentile(a.durations, .5)
		n.P95MS = percentile(a.durations, .95)
		r.Nodes = append(r.Nodes, *n)
	}
	outgoing := map[string]int{}
	for _, e := range edges {
		outgoing[e.From] += e.Count
	}
	for key, e := range edges {
		a := edgeAcc[key]
		e.Sessions = len(a.sessions)
		e.P95MS = percentile(a.durations, .95)
		e.Probability = float64(e.Count) / float64(outgoing[e.From])
		e.MeanGapMS /= float64(e.Count)
		r.Edges = append(r.Edges, *e)
	}
	for _, p := range paths {
		r.Paths = append(r.Paths, *p)
	}
	sort.Slice(r.Nodes, func(i, j int) bool {
		a, b := r.Nodes[i], r.Nodes[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.ID < b.ID
	})
	sort.Slice(r.Edges, func(i, j int) bool {
		a, b := r.Edges[i], r.Edges[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	sort.Slice(r.Paths, func(i, j int) bool {
		a, b := r.Paths[i], r.Paths[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return strings.Join(a.Nodes, "\x00") < strings.Join(b.Nodes, "\x00")
	})
	sort.Slice(r.Sessions, func(i, j int) bool { return r.Sessions[i].ID < r.Sessions[j].ID })
	return r, nil
}
