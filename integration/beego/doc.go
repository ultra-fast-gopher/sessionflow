// Package beego integrates sessionflow with Beego v2.
//
// Register FilterChain on a ControllerRegister before Init. Beego exposes the
// matched route as RouterPattern during the filter-chain unwind. Config.Session
// can read identity from *context.Context and runs only for admitted spans.
//
// Example:
//
//	app.InsertFilterChain("/*", beegoflow.FilterChain(rec, beegoflow.Config{
//	    Session: func(ctx *context.Context) string { return ctx.Input.GetData("user-id").(string) },
//	}))
package beego
