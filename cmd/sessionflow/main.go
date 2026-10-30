package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	flow "github.com/ultra-fast-gopher/sessionflow"
	"github.com/ultra-fast-gopher/sessionflow/analysis"
	"github.com/ultra-fast-gopher/sessionflow/report"
)

const help = `sessionflow: bounded session request-flow captures for ISUCON

  collect  -target http://host:6060/debug/sessionflow [-target ...] -duration 30s -out run.json
  start    -target URL -id run-id
  stop     -target URL -id run-id -out run.json
  status   -target URL
  analyze  -input run.json [-input ...] [-view summary|nodes|edges|paths|sessions|all]
           [-format json|text] [-session HASH] [-from 'GET /items'] [-to ROUTE] [-top 20] [-max-gap 30m]
  report   -input run.json [-input ...] -out report.html [-session HASH] [-max-gap 30m]
  serve    -input run.json [-input ...] [-addr 127.0.0.1:9070]
  demo     [-out demo.json]

JSON goes to stdout; progress and errors go to stderr. Set SESSIONFLOW_TOKEN for control authentication.
Targets are base URLs of the middleware control handler. Input can be a snapshot or collection bundle.
`

type stringsFlag []string

func (s *stringsFlag) String() string     { return strings.Join(*s, ",") }
func (s *stringsFlag) Set(v string) error { *s = append(*s, v); return nil }

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "sessionflow:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(help)
		return nil
	}
	switch args[0] {
	case "collect", "start", "stop", "status":
		return control(args[0], args[1:])
	case "analyze", "report", "serve":
		return inspect(args[0], args[1:])
	case "demo":
		return demo(args[1:])
	default:
		return fmt.Errorf("unknown command %q; use sessionflow help", args[0])
	}
}

func flags(name string) *flag.FlagSet { return flag.NewFlagSet(name, flag.ContinueOnError) }

func control(command string, args []string) error {
	f := flags(command)
	var targets stringsFlag
	f.Var(&targets, "target", "control base URL (repeat for collect)")
	id := f.String("id", "", "capture ID; collect generates one when omitted")
	out := f.String("out", "-", "output JSON file or - for stdout")
	duration := f.Duration("duration", 30*time.Second, "collection duration")
	timeout := f.Duration("timeout", 30*time.Second, "timeout for each control request")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() > 0 {
		return errors.New("unexpected positional arguments")
	}
	if len(targets) == 0 {
		return errors.New("at least one -target is required")
	}
	if command != "collect" && len(targets) != 1 {
		return errors.New("only collect accepts multiple targets")
	}
	if *timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	if command == "collect" && *duration <= 0 {
		return errors.New("duration must be positive")
	}
	unique := map[string]bool{}
	for i, target := range targets {
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("target must be an absolute HTTP(S) base URL without credentials, query or fragment")
		}
		targets[i] = strings.TrimRight(target, "/")
		if unique[targets[i]] {
			return errors.New("duplicate target")
		}
		unique[targets[i]] = true
	}
	if (command == "start" || command == "stop") && *id == "" {
		return errors.New("-id is required")
	}
	if len(*id) > 128 {
		return errors.New("id must be at most 128 bytes")
	}
	if command == "collect" && *id == "" {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return err
		}
		*id = time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
	}
	client := &http.Client{Timeout: *timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	if err := checkOutput(*out); err != nil {
		return err
	}
	if command != "collect" {
		var body []byte
		if command == "status" {
			var err error
			body, err = controlRequest(client, targets[0], command, "")
			if err != nil {
				return err
			}
		} else {
			var err error
			body, err = controlRequest(client, targets[0], command, *id)
			if err != nil {
				return err
			}
		}
		return writeOutput(*out, body)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	bundle := flow.Bundle{Version: flow.Version, Captures: []flow.Snapshot{}}
	var mu sync.Mutex
	active := make([]bool, len(targets))
	parallel := func(fn func(int, string)) {
		var wg sync.WaitGroup
		for i, t := range targets {
			wg.Add(1)
			go func(i int, t string) { defer wg.Done(); fn(i, t) }(i, t)
		}
		wg.Wait()
	}
	parallel(func(i int, target string) {
		_, err := controlRequest(client, target, "start", *id)
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			bundle.Errors = append(bundle.Errors, fmt.Sprintf("start %s: %v", target, err))
		} else {
			active[i] = true
		}
	})
	fmt.Fprintf(os.Stderr, "capture %s: %d targets, duration %s\n", *id, len(targets), *duration)
	if len(bundle.Errors) == 0 {
		timer := time.NewTimer(*duration)
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
	// Always stop successful starts, including rollback after a partial start.
	parallel(func(i int, target string) {
		if !active[i] {
			return
		}
		data, err := controlRequest(client, target, "stop", *id)
		var snapshot flow.Snapshot
		if err == nil {
			err = json.Unmarshal(data, &snapshot)
		}
		if err == nil {
			err = snapshot.Validate()
		}
		mu.Lock()
		defer mu.Unlock()
		if err != nil {
			bundle.Errors = append(bundle.Errors, fmt.Sprintf("stop %s (capture %s): %v", target, *id, err))
		} else {
			bundle.Captures = append(bundle.Captures, snapshot)
		}
	})
	sort.Slice(bundle.Captures, func(i, j int) bool { return bundle.Captures[i].Source < bundle.Captures[j].Source })
	sort.Strings(bundle.Errors)
	data, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	if err := writeOutput(*out, data); err != nil {
		return err
	}
	if len(bundle.Errors) > 0 {
		return fmt.Errorf("partial collection saved; %s", strings.Join(bundle.Errors, "; "))
	}
	return nil
}

func controlRequest(client *http.Client, target, action, id string) ([]byte, error) {
	method := http.MethodPost
	data, _ := json.Marshal(map[string]string{"capture_id": id})
	if action == "status" {
		method = http.MethodGet
		data = nil
	}
	req, err := http.NewRequest(method, target+"/"+action, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := os.Getenv("SESSIONFLOW_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	const limit = 128 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, errors.New("capture exceeds 128 MiB response limit; reduce MaxEvents")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(body) > 1024 {
			body = body[:1024]
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func load(inputs []string) ([]flow.Snapshot, []string, error) {
	if len(inputs) == 0 {
		return nil, nil, errors.New("at least one -input is required")
	}
	var captures []flow.Snapshot
	var warnings []string
	for _, path := range inputs {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, err
		}
		var envelope struct {
			Version  int             `json:"version"`
			Captures json.RawMessage `json:"captures"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		if envelope.Version != flow.Version {
			return nil, nil, fmt.Errorf("%s: unsupported version %d", path, envelope.Version)
		}
		if envelope.Captures != nil {
			var b flow.Bundle
			if err := json.Unmarshal(data, &b); err != nil {
				return nil, nil, err
			}
			captures = append(captures, b.Captures...)
			warnings = append(warnings, b.Errors...)
		} else {
			var s flow.Snapshot
			if err := json.Unmarshal(data, &s); err != nil {
				return nil, nil, err
			}
			captures = append(captures, s)
		}
	}
	return captures, warnings, nil
}

func inspect(command string, args []string) error {
	f := flags(command)
	var inputs stringsFlag
	f.Var(&inputs, "input", "capture or bundle JSON (repeatable)")
	view := f.String("view", "summary", "JSON view: summary, nodes, edges, paths, sessions, all")
	format := f.String("format", "json", "json or text")
	session := f.String("session", "", "filter by session hash or full session ID")
	from := f.String("from", "", "exact edge source node")
	to := f.String("to", "", "exact edge destination node")
	top := f.Int("top", 0, "limit nodes, edges, paths, and sessions; 0 means all")
	maxGap := f.Duration("max-gap", 30*time.Minute, "break a sequence after this idle gap")
	out := f.String("out", "report.html", "HTML output path")
	addr := f.String("addr", "127.0.0.1:9070", "report HTTP listen address")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() > 0 {
		return errors.New("unexpected positional arguments")
	}
	if *top < 0 || *maxGap <= 0 {
		return errors.New("top must be nonnegative and max-gap must be positive")
	}
	captures, warnings, err := load(inputs)
	if err != nil {
		return err
	}
	r, err := analysis.Analyze(captures, analysis.Options{MaxGap: *maxGap, Session: *session})
	if err != nil {
		return err
	}
	r.Summary.Warnings = append(r.Summary.Warnings, warnings...)
	if command == "report" || command == "serve" {
		var page bytes.Buffer
		if err := report.Write(&page, r); err != nil {
			return err
		}
		if command == "report" {
			return writeOutput(*out, page.Bytes())
		}
		listener, err := net.Listen("tcp", *addr)
		if err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "report: http://"+listener.Addr().String())
		return http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path != "/" {
				http.NotFound(w, req)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write(page.Bytes())
		}))
	}
	if *from != "" || *to != "" {
		edges := []analysis.Edge{}
		for _, e := range r.Edges {
			if (*from == "" || e.From == *from) && (*to == "" || e.To == *to) {
				edges = append(edges, e)
			}
		}
		r.Edges = edges
	}
	if *top > 0 {
		if len(r.Nodes) > *top {
			r.Nodes = r.Nodes[:*top]
		}
		if len(r.Edges) > *top {
			r.Edges = r.Edges[:*top]
		}
		if len(r.Paths) > *top {
			r.Paths = r.Paths[:*top]
		}
		if len(r.Sessions) > *top {
			r.Sessions = r.Sessions[:*top]
		}
	}
	validViews := map[string]bool{"summary": true, "nodes": true, "edges": true, "paths": true, "sessions": true, "all": true}
	if !validViews[*view] {
		return errors.New("unknown view")
	}
	if *format == "text" {
		fmt.Printf("captures=%d events=%d sessions=%d transitions=%d overlaps=%d dropped=%d in_flight=%d\n", r.Summary.Captures, r.Summary.Events, r.Summary.Sessions, r.Summary.Transitions, r.Summary.Overlaps, r.Summary.Dropped, r.Summary.InFlight)
		for _, w := range r.Summary.Warnings {
			fmt.Println("warning:", w)
		}
		if *view == "edges" || *view == "all" {
			for _, e := range r.Edges {
				fmt.Printf("%6d  %6.1f%%  p95=%8.3fms  errors=%d  %s -> %s\n", e.Count, e.Probability*100, e.P95MS, e.Errors, e.From, e.To)
			}
		}
		if *view == "nodes" || *view == "all" {
			for _, n := range r.Nodes {
				fmt.Printf("%6d  sessions=%d  p95=%8.3fms  errors=%d  %s\n", n.Count, n.Sessions, n.P95MS, n.Errors, n.ID)
			}
		}
		if *view == "paths" || *view == "all" {
			for _, p := range r.Paths {
				fmt.Printf("%6d  %s\n", p.Count, strings.Join(p.Nodes, " -> "))
			}
		}
		if *view == "sessions" || *view == "all" {
			for _, s := range r.Sessions {
				fmt.Printf("session %s (%d requests)\n", s.ID, len(s.Requests))
				for _, req := range s.Requests {
					fmt.Printf("  %s  %s  %d  %.3fms  %s\n", time.Unix(0, req.StartNS).UTC().Format(time.RFC3339Nano), req.Node, req.Status, float64(req.DurationNS)/1e6, req.Relation)
				}
			}
		}
		return nil
	}
	if *format != "json" {
		return errors.New("format must be json or text")
	}
	var value any
	switch *view {
	case "summary":
		value = r.Summary
	case "nodes":
		value = r.Nodes
	case "edges":
		value = r.Edges
	case "paths":
		value = r.Paths
	case "sessions":
		value = r.Sessions
	case "all":
		value = r
	default:
		return errors.New("unknown view")
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func checkOutput(path string) error {
	if path == "-" {
		return nil
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return errors.New("output path is a directory")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sessionflow-check-*")
	if err != nil {
		return err
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

func writeOutput(path string, data []byte) error {
	if len(data) == 0 || data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	if path == "-" {
		_, err := os.Stdout.Write(data)
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".sessionflow-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func demo(args []string) error {
	f := flags("demo")
	out := f.String("out", "demo.json", "output capture")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() > 0 {
		return errors.New("unexpected positional arguments")
	}
	r, err := flow.New(flow.Options{Source: "demo", HashKey: []byte("demo-only-key"), MaxEvents: 5000, Include: flow.DefaultInclude})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	for _, route := range []string{"GET /", "POST /login", "GET /items/{id}", "POST /cart", "POST /checkout"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, req *http.Request) {
			time.Sleep(time.Millisecond)
			if req.URL.Path == "/checkout" && req.Header.Get("X-Demo-Fail") == "1" {
				w.WriteHeader(500)
			}
			_, _ = w.Write([]byte("ok"))
		})
	}
	handler := r.Wrap(mux)
	if err = r.Start("demo"); err != nil {
		return err
	}
	var wg sync.WaitGroup
	for user := 0; user < 24; user++ {
		wg.Add(1)
		go func(user int) {
			defer wg.Done()
			routes := []string{"GET /", "POST /login", "GET /items/42", "POST /cart", "POST /checkout"}
			if user%4 == 0 {
				routes = routes[:3]
			}
			for _, route := range routes {
				method, path, _ := strings.Cut(route, " ")
				req := httptest.NewRequest(method, path, nil)
				req.AddCookie(&http.Cookie{Name: "session", Value: fmt.Sprint(user)})
				if user%5 == 0 {
					req.Header.Set("X-Demo-Fail", "1")
				}
				handler.ServeHTTP(httptest.NewRecorder(), req)
			}
		}(user)
	}
	wg.Wait()
	snapshot, err := r.Stop("demo")
	if err != nil {
		return err
	}
	data, err := json.Marshal(flow.Bundle{Version: flow.Version, Captures: []flow.Snapshot{snapshot}})
	if err != nil {
		return err
	}
	return writeOutput(*out, data)
}
