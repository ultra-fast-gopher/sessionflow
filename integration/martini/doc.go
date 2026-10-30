// Package martini integrates sessionflow with Martini.
//
// Put Middleware in each route handler list. Martini injects the matched native
// route only after route selection, so global m.Use cannot supply its template.
// Config.Session receives Martini's request context and request, and runs only
// while a capture is active.
//
// Example:
//
//	m.Get("/users/:id", martiniflow.Middleware(rec, martiniflow.Config{
//	    Session: func(ctx martini.Context, req *http.Request) string { return "" },
//	}), handler)
package martini
