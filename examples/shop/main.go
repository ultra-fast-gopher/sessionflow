// A runnable instrumentation example, with load generation for end-to-end checks.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	flow "github.com/ultra-fast-gopher/sessionflow"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "application listen address")
	debug := flag.String("debug-addr", "127.0.0.1:16060", "control listen address")
	load := flag.String("load", "", "generate traffic against this app URL instead of serving")
	users := flag.Int("users", 20, "number of concurrent synthetic sessions")
	duration := flag.Duration("duration", 10*time.Second, "load duration")
	flag.Parse()
	if *load != "" {
		if *users < 1 || *duration <= 0 {
			log.Fatal("users and duration must be positive")
		}
		if err := traffic(*load, *users, *duration); err != nil {
			log.Fatal(err)
		}
		return
	}
	r, err := flow.New(flow.Options{CookieName: "session", HashKey: []byte(os.Getenv("SESSIONFLOW_HASH_KEY")), MaxEvents: 100000})
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	for _, route := range []string{"GET /", "POST /login", "GET /items/{id}", "POST /cart", "POST /checkout"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, req *http.Request) {
			time.Sleep(time.Millisecond)
			if req.URL.Path == "/checkout" && req.Header.Get("X-Demo-Fail") == "1" {
				w.WriteHeader(http.StatusInternalServerError)
			}
			_, _ = fmt.Fprintln(w, "ok")
		})
	}
	go func() {
		log.Printf("control: http://%s/debug/sessionflow", *debug)
		private := http.NewServeMux()
		private.Handle("/debug/sessionflow/", http.StripPrefix("/debug/sessionflow", r.ControlHandler(os.Getenv("SESSIONFLOW_TOKEN"))))
		log.Fatal(http.ListenAndServe(*debug, private))
	}()
	log.Printf("app: http://%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, r.Wrap(mux)))
}

func traffic(target string, users int, duration time.Duration) error {
	client := &http.Client{Timeout: 5 * time.Second}
	deadline := time.Now().Add(duration)
	var wg sync.WaitGroup
	failures := make(chan error, users)
	for user := 0; user < users; user++ {
		wg.Add(1)
		go func(user int) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				for _, route := range []string{"GET /", "POST /login", "GET /items/42", "POST /cart", "POST /checkout"} {
					method, path, _ := strings.Cut(route, " ")
					req, err := http.NewRequest(method, strings.TrimRight(target, "/")+path, nil)
					if err != nil {
						failures <- err
						return
					}
					req.AddCookie(&http.Cookie{Name: "session", Value: fmt.Sprint(user)})
					if user%5 == 0 {
						req.Header.Set("X-Demo-Fail", "1")
					}
					resp, err := client.Do(req)
					if err != nil {
						failures <- err
						return
					}
					_, err = io.Copy(io.Discard, resp.Body)
					_ = resp.Body.Close()
					if err != nil {
						failures <- err
						return
					}
				}
			}
		}(user)
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		return err
	}
	return nil
}
