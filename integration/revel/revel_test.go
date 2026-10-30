package revel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/revel/pathtree"
	"github.com/revel/revel"
	"github.com/revel/revel/model"
	"github.com/ultra-fast-gopher/sessionflow"
)

type usersController struct{ revel.Controller }

func (*usersController) Show() revel.Result { return fixedResult{} }

type fixedResult struct{}

func (fixedResult) Apply(_ *revel.Request, resp *revel.Response) {
	resp.WriteHeader(http.StatusTeapot, "text/plain")
	_, _ = io.WriteString(resp.GetWriter(), "tea")
}

func TestFilterObservesRoutedResultAndUniqueTemplate(t *testing.T) {
	oldRouter := revel.MainRouter
	oldConfig := revel.RevelConfig
	oldFilters := append([]revel.Filter(nil), revel.Filters...)
	t.Cleanup(func() { revel.MainRouter = oldRouter; revel.RevelConfig = oldConfig; revel.Filters = oldFilters })
	revel.RevelConfig = &model.RevelContainer{}
	revel.RegisterController((*usersController)(nil), []*revel.MethodType{{Name: "Show"}})
	controllerType := revel.ControllerTypeByName("usersController", nil)
	route := &revel.Route{ControllerName: "usersController", MethodName: "Show", Method: "GET", Path: "/users/:id", TypeOfController: controllerType}
	tree := pathtree.New()
	if err := tree.Add("/GET/users/:id", []*revel.Route{route}); err != nil {
		t.Fatal(err)
	}
	revel.MainRouter = &revel.Router{Tree: tree, Routes: []*revel.Route{route}}

	rec, err := sessionflow.New(sessionflow.Options{Source: "revel-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Start("revel-capture"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/users/42", nil)
	serverContext := revel.NewGoContext(nil)
	serverContext.Request.SetRequest(req)
	serverContext.Response.SetResponse(w)
	c := revel.NewController(serverContext)
	filter := Filter(rec, nil, Config{Session: func(c *revel.Controller) string { return "private-session" }})
	filter(c, []revel.Filter{
		revel.RouterFilter,
		func(c *revel.Controller, _ []revel.Filter) {
			if c.Params.Route.Get("id") != "42" {
				t.Errorf("route params = %#v", c.Params.Route)
			}
			c.Result = fixedResult{}
		},
	})
	if c.Action != "usersController.Show" {
		t.Fatalf("action = %q", c.Action)
	}
	if c.Result == nil {
		t.Fatal("filter did not retain the controller result")
	}
	c.Result.Apply(c.Request, c.Response)
	if w.Code != http.StatusTeapot || w.Body.String() != "tea" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	snap, err := rec.Stop("revel-capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.Validate(); err != nil {
		t.Fatalf("invalid snapshot: %v", err)
	}
	if len(snap.Events) != 1 {
		t.Fatalf("events = %d", len(snap.Events))
	}
	e := snap.Events[0]
	if e.Route != "/users/:id" || e.Status != w.Code || e.BytesUnknown || e.Bytes != int64(w.Body.Len()) || e.DurationNS <= 0 || e.Session == "private-session" || len(e.Session) != 64 {
		t.Fatalf("event = %#v", e)
	}
}

type panicResult struct{}

func (panicResult) Apply(_ *revel.Request, resp *revel.Response) {
	_, _ = io.WriteString(resp.GetWriter(), "partial")
	panic("result panic")
}

func TestResultPanicIsRecordedAndPropagated(t *testing.T) {
	oldRouter := revel.MainRouter
	oldConfig := revel.RevelConfig
	t.Cleanup(func() { revel.MainRouter = oldRouter; revel.RevelConfig = oldConfig })
	revel.RevelConfig = &model.RevelContainer{}
	revel.RegisterController((*usersController)(nil), []*revel.MethodType{{Name: "Show"}})
	route := &revel.Route{ControllerName: "usersController", MethodName: "Show", Method: "GET", Path: "/panic/:id", TypeOfController: revel.ControllerTypeByName("usersController", nil)}
	tree := pathtree.New()
	if err := tree.Add("/GET/panic/:id", []*revel.Route{route}); err != nil {
		t.Fatal(err)
	}
	revel.MainRouter = &revel.Router{Tree: tree, Routes: []*revel.Route{route}}
	rec, err := sessionflow.New(sessionflow.Options{Source: "revel-panic-test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Start("revel-panic-capture"); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	serverContext := revel.NewGoContext(nil)
	serverContext.Request.SetRequest(httptest.NewRequest("GET", "/panic/42", nil))
	serverContext.Response.SetResponse(w)
	c := revel.NewController(serverContext)
	Filter(rec, nil)(c, []revel.Filter{revel.RouterFilter, func(c *revel.Controller, _ []revel.Filter) { c.Result = panicResult{} }})
	panicked := false
	func() {
		defer func() { panicked = recover() != nil }()
		c.Result.Apply(c.Request, c.Response)
	}()
	if !panicked {
		t.Fatal("Result.Apply panic was swallowed")
	}
	snap, err := rec.Stop("revel-panic-capture")
	if err != nil {
		t.Fatal(err)
	}
	if err := snap.Validate(); err != nil {
		t.Fatalf("invalid snapshot: %v", err)
	}
	if len(snap.Events) != 1 || !snap.Events[0].Panicked || snap.Events[0].Status != 0 {
		t.Fatalf("events = %#v", snap.Events)
	}
}

func TestDisabledSkipsNativeSessionCallback(t *testing.T) {
	oldRouter := revel.MainRouter
	oldConfig := revel.RevelConfig
	t.Cleanup(func() { revel.MainRouter = oldRouter; revel.RevelConfig = oldConfig })
	revel.RevelConfig = &model.RevelContainer{}
	revel.RegisterController((*usersController)(nil), []*revel.MethodType{{Name: "Show"}})
	route := &revel.Route{ControllerName: "usersController", MethodName: "Show", Method: "GET", Path: "/health", TypeOfController: revel.ControllerTypeByName("usersController", nil)}
	tree := pathtree.New()
	if err := tree.Add("/GET/health", []*revel.Route{route}); err != nil {
		t.Fatal(err)
	}
	revel.MainRouter = &revel.Router{Tree: tree, Routes: []*revel.Route{route}}
	rec, err := sessionflow.New(sessionflow.Options{})
	if err != nil {
		t.Fatal(err)
	}
	called := false
	w := httptest.NewRecorder()
	serverContext := revel.NewGoContext(nil)
	serverContext.Request.SetRequest(httptest.NewRequest("GET", "/health", nil))
	serverContext.Response.SetResponse(w)
	c := revel.NewController(serverContext)
	Filter(rec, nil, Config{Session: func(*revel.Controller) string { called = true; return "native-session" }})(c, []revel.Filter{
		revel.RouterFilter,
		func(c *revel.Controller, _ []revel.Filter) { c.Result = fixedResult{} },
	})
	c.Result.Apply(c.Request, c.Response)
	if called || w.Code != http.StatusTeapot || w.Body.String() != "tea" {
		t.Fatalf("callback=%v response=%d %q", called, w.Code, w.Body.String())
	}
}
