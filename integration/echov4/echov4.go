// Package echov4 adapts Echo v4 requests to sessionflow captures. Set the
// application's HTTPErrorHandler first, then call Install(e, recorder) before
// serving so returned errors are captured after the real error response.
package echov4

import (
	"sync"
	"sync/atomic"

	"github.com/labstack/echo/v4"
	flow "github.com/ultra-fast-gopher/sessionflow"
)

const spanKey = "sessionflow.echo.v4.span"

type Config struct {
	// Session returns an authenticated user or session identifier after handlers run.
	Session func(echo.Context) string
	// Route can replace Echo's matched route template.
	Route func(echo.Context, string) string
}

type pending struct {
	span     *flow.Span
	rec      *flow.Recorder
	cfg      Config
	routes   *routeIndex
	finished atomic.Bool
}

// Middleware records Echo's matched route template, final status and response size.
// InstallErrorHandler is also required to finish spans for returned handler errors.
// Register routes before serving; the static-route lookup is cached on the first
// active request, with separate cached sets for Echo host routers.
func Middleware(rec *flow.Recorder, configs ...Config) echo.MiddlewareFunc {
	cfg := Config{}
	if len(configs) > 0 {
		cfg = configs[0]
	}
	var routes routeIndex
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			span := rec.BeginRequest(c.Request())
			if span == nil {
				return next(c)
			}
			p := &pending{span: span, rec: rec, cfg: cfg, routes: &routes}
			c.Set(spanKey, p)
			completed := false
			defer func() {
				if !completed {
					finish(c, p, true, &routes)
				}
			}()
			err := next(c)
			completed = true
			if err != nil {
				return err
			}
			finish(c, p, false, &routes)
			return nil
		}
	}
}

// InstallErrorHandler wraps the configured handler and records its final response.
// Call before serving requests, after configuring the application's error handler.
func InstallErrorHandler(e *echo.Echo) {
	original := e.HTTPErrorHandler
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		panicked := true
		defer func() {
			if panicked {
				if p, ok := c.Get(spanKey).(*pending); ok {
					finish(c, p, true, p.routes)
				}
			}
		}()
		if original != nil {
			original(err, c)
		} else {
			e.DefaultHTTPErrorHandler(err, c)
		}
		if p, ok := c.Get(spanKey).(*pending); ok {
			finish(c, p, false, p.routes)
		}
		panicked = false
	}
}

// Install registers the middleware and wraps the configured error handler.
// Call after configuring the application's error handler and before serving.
func Install(e *echo.Echo, rec *flow.Recorder, configs ...Config) {
	InstallErrorHandler(e)
	e.Use(Middleware(rec, configs...))
}

type routeSet map[string]map[string]struct{}

func newRouteSet(routes []*echo.Route) routeSet {
	set := make(routeSet)
	for _, route := range routes {
		paths := set[route.Method]
		if paths == nil {
			paths = make(map[string]struct{})
			set[route.Method] = paths
		}
		paths[route.Path] = struct{}{}
	}
	return set
}

func (s routeSet) contains(method, path string) bool { _, ok := s[method][path]; return ok }

type routeIndex struct {
	apps sync.Map // map[*echo.Echo]*routeCache
}

type routeCache struct {
	once          sync.Once
	defaultRoutes routeSet
	hostRoutes    map[string]routeSet
}

func (i *routeIndex) contains(e *echo.Echo, host, method, path string) bool {
	value, ok := i.apps.Load(e)
	if !ok {
		value, _ = i.apps.LoadOrStore(e, &routeCache{})
	}
	cache := value.(*routeCache)
	cache.once.Do(func() {
		cache.defaultRoutes = newRouteSet(e.Router().Routes())
		cache.hostRoutes = make(map[string]routeSet, len(e.Routers()))
		for host, router := range e.Routers() {
			cache.hostRoutes[host] = newRouteSet(router.Routes())
		}
	})
	set := cache.defaultRoutes
	if hostSet, ok := cache.hostRoutes[host]; ok {
		set = hostSet
	}
	return set.contains(method, path)
}

func finish(c echo.Context, p *pending, panicked bool, routes *routeIndex) {
	if !p.finished.CompareAndSwap(false, true) {
		return
	}
	req := c.Request()
	if panicked {
		p.span.End(flow.Observation{Method: req.Method, Route: "<unmatched>", Status: c.Response().Status, Bytes: c.Response().Size, Panicked: true})
		return
	}
	route := c.Path()
	if route == req.URL.Path {
		if routes == nil || !routes.contains(c.Echo(), req.Host, req.Method, route) {
			route = ""
		}
	}
	if p.cfg.Route != nil {
		route = p.cfg.Route(c, route)
	}
	session := ""
	if p.cfg.Session != nil {
		session = p.cfg.Session(c)
	} else {
		session = p.rec.SessionID(req)
	}
	response := c.Response()
	p.span.End(flow.Observation{Method: req.Method, Route: p.rec.RoutePattern(req, route), Session: session, Status: response.Status, Bytes: response.Size, Panicked: panicked})
}
