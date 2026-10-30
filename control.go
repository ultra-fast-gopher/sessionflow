package sessionflow

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// ControlHandler serves POST /start, POST /stop and GET /status. Mount this on
// a private debug listener. A nonempty token requires Bearer authentication.
// The last stopped capture can be retrieved again until the next Start.
func (r *Recorder) ControlHandler(token string) http.Handler {
	mux := http.NewServeMux()
	type request struct {
		CaptureID string `json:"capture_id"`
	}
	decode := func(w http.ResponseWriter, req *http.Request) (string, bool) {
		var body request
		decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, 4096))
		if err := decoder.Decode(&body); err != nil || body.CaptureID == "" || len(body.CaptureID) > 128 {
			http.Error(w, "expected capture_id (1..128 bytes)", http.StatusBadRequest)
			return "", false
		}
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			http.Error(w, "expected one JSON object", http.StatusBadRequest)
			return "", false
		}
		return body.CaptureID, true
	}
	mux.HandleFunc("POST /start", func(w http.ResponseWriter, req *http.Request) {
		id, ok := decode(w, req)
		if !ok {
			return
		}
		if err := r.Start(id); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(r.Status())
	})
	mux.HandleFunc("POST /stop", func(w http.ResponseWriter, req *http.Request) {
		id, ok := decode(w, req)
		if !ok {
			return
		}
		snapshot, err := r.Stop(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(snapshot)
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(r.Status())
	})
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if token != "" && subtle.ConstantTimeCompare([]byte(req.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// A trailing slash is accepted without a redirect of a POST body.
		req.URL.Path = strings.TrimSuffix(req.URL.Path, "/")
		mux.ServeHTTP(w, req)
	})
}
