// Package macaron integrates sessionflow with Macaron v1.
//
// Add Middleware to a route's handler list and pass the registered route pattern;
// Macaron's Context does not expose its selected route template. Config.Session
// can read authenticated identity or route parameters from *macaron.Context.
//
// Example:
//
//	m.Get("/users/:id", macaronflow.Middleware(rec, "/users/:id", macaronflow.Config{
//	    Session: func(ctx *macaron.Context) string { return ctx.Data["user-id"].(string) },
//	}), handler)
package macaron
