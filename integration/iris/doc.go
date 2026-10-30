// Package iris integrates sessionflow with Iris v12.
//
// Register Middleware with Application.UseRouter so the matched Iris route and
// unmatched 404 responses are observed after the handler chain completes. Use
// Config.Session when authenticated identity lives in Iris context values; it is
// called only while a capture is active.
//
// Example:
//
//	app.UseRouter(irisflow.Middleware(rec, irisflow.Config{
//	    Session: func(ctx iris.Context) string { return ctx.Values().GetString("user-id") },
//	}))
package iris
