package echov4

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

func BenchmarkEchoMiddleware(b *testing.B) {
	for _, mode := range []string{"baseline", "disabled", "recording"} {
		b.Run(mode, func(b *testing.B) {
			r, err := flow.New(flow.Options{Source: "bench", HashKey: []byte("bench-key"), MaxEvents: 512})
			if err != nil {
				b.Fatal(err)
			}
			e := echo.New()
			if mode != "baseline" {
				e.Use(Middleware(r))
			}
			e.GET("/ping", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
			req := httptest.NewRequest(http.MethodGet, "/ping", nil)
			req.AddCookie(&http.Cookie{Name: "session", Value: "bench-session"})
			const batch = 256
			for done := 0; done < b.N; {
				n := batch
				if b.N-done < n {
					n = b.N - done
				}
				b.StopTimer()
				if mode == "recording" {
					if err := r.Start("batch"); err != nil {
						b.Fatal(err)
					}
				}
				b.StartTimer()
				for i := 0; i < n; i++ {
					e.ServeHTTP(httptest.NewRecorder(), req)
				}
				b.StopTimer()
				if mode == "recording" {
					if _, err := r.Stop("batch"); err != nil {
						b.Fatal(err)
					}
				}
				done += n
			}
		})
	}
}

func BenchmarkEchoStaticRouteIndex(b *testing.B) {
	for _, routeCount := range []int{1, 128} {
		b.Run(fmt.Sprintf("routes-%d", routeCount), func(b *testing.B) {
			r, err := flow.New(flow.Options{Source: "bench", HashKey: []byte("bench-key"), MaxEvents: 256})
			if err != nil {
				b.Fatal(err)
			}
			e := echo.New()
			e.Use(Middleware(r))
			e.GET("/target", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
			for i := 1; i < routeCount; i++ {
				path := fmt.Sprintf("/static-%d", i)
				e.GET(path, func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })
			}
			req := httptest.NewRequest(http.MethodGet, "/target", nil)
			req.AddCookie(&http.Cookie{Name: "session", Value: "bench-session"})
			b.StopTimer()
			if err := r.Start("warmup"); err != nil {
				b.Fatal(err)
			}
			e.ServeHTTP(httptest.NewRecorder(), req)
			if _, err := r.Stop("warmup"); err != nil {
				b.Fatal(err)
			}
			const batch = 128
			for done := 0; done < b.N; {
				n := batch
				if b.N-done < n {
					n = b.N - done
				}
				b.StopTimer()
				if err := r.Start("batch"); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				for i := 0; i < n; i++ {
					e.ServeHTTP(httptest.NewRecorder(), req)
				}
				b.StopTimer()
				if _, err := r.Stop("batch"); err != nil {
					b.Fatal(err)
				}
				done += n
			}
		})
	}
}
