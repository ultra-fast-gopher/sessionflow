// Package revel integrates sessionflow with Revel v1.
//
// Prepend Filter to revel.Filters so it wraps routing and action execution.
// The adapter observes the Result when Revel applies it, preserving final status
// and body byte counts. Revel BinaryResult uses its stream writer directly, so
// its byte count is marked unknown. RouteTemplate resolves actions with one
// registered route; pass a route callback when an action has aliases.
// Config.Session can read the controller session after action execution.
//
// Example:
//
//	revel.Filters = append([]revel.Filter{revelflow.Filter(rec, nil, revelflow.Config{
//	    Session: func(c *revel.Controller) string { return c.Session["user_id"] },
//	})}, revel.Filters...)
package revel
