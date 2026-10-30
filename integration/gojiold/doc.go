// Package gojiold integrates sessionflow with github.com/zenazn/goji v0.9.
//
// Use Handler to wrap a native web.Handler so Goji's web.C and URLParams are
// preserved. Pass the registered template because this release does not expose
// the selected pattern. Middleware is a convenience for downstream net/http
// handlers. Config.Session reads Goji context and runs only during active capture.
//
// Example:
//
//	mux.Get("/users/:id", gojiflow.Handler(rec, "/users/:id", web.HandlerFunc(handler), gojiflow.Config{
//	    Session: func(c web.C, req *http.Request) string { return c.URLParams["user"] },
//	}))
package gojiold
