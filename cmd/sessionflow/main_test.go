package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	flow "github.com/ultra-fast-gopher/sessionflow"
)

func controlServer(t *testing.T, source string) (*flow.Recorder, *httptest.Server) {
	t.Helper()
	r, err := flow.New(flow.Options{Source: source, MaxEvents: 10, HashKey: []byte("shared-key")})
	if err != nil {
		t.Fatal(err)
	}
	control := r.ControlHandler("")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	app := r.Wrap(mux)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		control.ServeHTTP(w, req)
		if req.URL.Path == "/start" {
			r := httptest.NewRequest("GET", "/items/42", nil)
			r.AddCookie(&http.Cookie{Name: "session", Value: "one"})
			app.ServeHTTP(httptest.NewRecorder(), r)
		}
	}))
	t.Cleanup(server.Close)
	return r, server
}

func TestCollectTwoServersAndAnalyze(t *testing.T) {
	t.Setenv("SESSIONFLOW_TOKEN", "")
	a, sa := controlServer(t, "a")
	b, sb := controlServer(t, "b")
	path := filepath.Join(t.TempDir(), "run.json")
	if err := run([]string{"collect", "-target", sa.URL, "-target", sb.URL, "-duration", "5ms", "-id", "test", "-out", path}); err != nil {
		t.Fatal(err)
	}
	if a.Status().Active || b.Status().Active {
		t.Fatal("capture left active")
	}
	captures, warnings, err := load([]string{path})
	if err != nil || len(warnings) != 0 || len(captures) != 2 {
		t.Fatal(captures, warnings, err)
	}
	for _, s := range captures {
		if s.CaptureID != "test" || len(s.Events) != 1 || s.Events[0].Route != "/items/{id}" {
			t.Fatal(s)
		}
	}
	page := filepath.Join(t.TempDir(), "report.html")
	if err := run([]string{"report", "-input", path, "-out", page}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(page)
	if err != nil || !strings.Contains(string(data), "GET /items/{id}") {
		t.Fatal("missing report", err)
	}
}

func TestPartialStartRollsBackAndPreservesSuccessfulCapture(t *testing.T) {
	t.Setenv("SESSIONFLOW_TOKEN", "")
	r, good := controlServer(t, "good")
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { http.Error(w, "unavailable", 503) }))
	defer bad.Close()
	path := filepath.Join(t.TempDir(), "partial.json")
	err := run([]string{"collect", "-target", good.URL, "-target", bad.URL, "-duration", "1h", "-out", path})
	if err == nil {
		t.Fatal("partial collection returned success")
	}
	if r.Status().Active {
		t.Fatal("rollback did not stop capture")
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var b flow.Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Captures) != 1 || len(b.Errors) != 1 {
		t.Fatal(b)
	}
}

func TestCLIRejectsBadArgumentsBeforeCapture(t *testing.T) {
	for _, args := range [][]string{
		{"collect", "-target", "file:///tmp/no"},
		{"collect", "-target", "http://localhost", "-duration", "0s"},
		{"collect", "-target", "http://localhost", "-target", "http://localhost/"},
		{"stop", "-target", "http://localhost"},
		{"collect", "-target", "http://localhost", "-out", filepath.Join(t.TempDir(), "missing", "run.json")},
	} {
		if err := run(args); err == nil {
			t.Fatal("accepted invalid arguments", args)
		}
	}
}
