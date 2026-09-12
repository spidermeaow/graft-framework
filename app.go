package graft

import (
	"net/http"
	"strings"
	"sync"
)

// Handler handles a request; returned errors go to the application's ErrorHandler.
type Handler func(*Context) error

// Middleware is compatible with standard net/http middleware. First added runs first.
type Middleware func(http.Handler) http.Handler

// App is an http.Handler. Configure routes and middleware before serving requests.
type App struct {
	mux          *http.ServeMux
	mu           sync.Mutex
	once         sync.Once
	serving      bool
	handler      http.Handler
	middleware   []Middleware
	errorHandler ErrorHandler
	routes       []routeDoc
}

// New creates an app with panic recovery. Logging, request IDs and docs are opt-in.
func New() *App {
	return &App{mux: http.NewServeMux(), errorHandler: DefaultErrorHandler}
}

func (a *App) configure(fn func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.serving {
		panic("graft: configure application before serving requests")
	}
	fn()
}

// Use adds application middleware in execution order.
func (a *App) Use(m ...Middleware) { a.configure(func() { a.middleware = append(a.middleware, m...) }) }

// SetErrorHandler replaces the centralized error handler.
func (a *App) SetErrorHandler(h ErrorHandler) {
	if h == nil {
		panic("graft: nil error handler")
	}
	a.configure(func() { a.errorHandler = h })
}

// ServeHTTP implements http.Handler and freezes the application configuration.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.once.Do(func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.serving = true
		// Recover handler/group panics inside application middleware so request
		// loggers observe the rendered status. The outer guard covers middleware.
		a.handler = Recovery()(chain(Recovery()(a.mux), a.middleware))
	})
	w = track(w)
	r = withErrorHandler(r, a.errorHandler)
	a.handler.ServeHTTP(w, r)
}

func chain(h http.Handler, m []Middleware) http.Handler {
	for i := len(m) - 1; i >= 0; i-- {
		h = m[i](h)
	}
	return h
}

// Handle registers a method and a Go ServeMux path pattern. Invalid or conflicting
// patterns panic, just as they do with http.ServeMux.
func (a *App) Handle(method, path string, h Handler, opts ...RouteOption) {
	a.add(method, path, h, nil, opts)
}

func (a *App) add(method, path string, h Handler, middleware []Middleware, opts []RouteOption) {
	if h == nil {
		panic("graft: nil handler")
	}
	if method == "" || strings.ContainsAny(method, " \t\r\n") || !strings.HasPrefix(path, "/") {
		panic("graft: invalid method or path")
	}
	a.configure(func() {
		doc := newRouteDoc(method, path, opts)
		a.mux.Handle(method+" "+path, chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := &Context{request: r, response: w}
			if err := h(c); err != nil {
				a.errorHandler(c, err)
			}
		}), middleware))
		a.routes = append(a.routes, doc)
	})
}

// GET registers a GET route (also serving HEAD, following net/http semantics).
func (a *App) GET(p string, h Handler, o ...RouteOption) { a.Handle(http.MethodGet, p, h, o...) }

// POST registers a POST route.
func (a *App) POST(p string, h Handler, o ...RouteOption) { a.Handle(http.MethodPost, p, h, o...) }

// PUT registers a PUT route.
func (a *App) PUT(p string, h Handler, o ...RouteOption) { a.Handle(http.MethodPut, p, h, o...) }

// PATCH registers a PATCH route.
func (a *App) PATCH(p string, h Handler, o ...RouteOption) { a.Handle(http.MethodPatch, p, h, o...) }

// DELETE registers a DELETE route.
func (a *App) DELETE(p string, h Handler, o ...RouteOption) { a.Handle(http.MethodDelete, p, h, o...) }

// Group is a route prefix with middleware. Middleware is copied at registration.
type Group struct {
	app        *App
	prefix     string
	middleware []Middleware
}

// Group creates a route group. Prefixes must start with a slash.
func (a *App) Group(prefix string, m ...Middleware) *Group {
	if !strings.HasPrefix(prefix, "/") {
		panic("graft: group prefix must start with /")
	}
	return &Group{app: a, prefix: strings.TrimSuffix(prefix, "/"), middleware: append([]Middleware(nil), m...)}
}

// Group creates a nested group, inheriting the parent's middleware.
func (g *Group) Group(prefix string, m ...Middleware) *Group {
	return g.app.Group(g.prefix+prefix, append(append([]Middleware(nil), g.middleware...), m...)...)
}

// Handle registers a route within this group.
func (g *Group) Handle(method, p string, h Handler, o ...RouteOption) {
	g.app.add(method, g.prefix+p, h, g.middleware, o)
}

// GET registers a group GET route.
func (g *Group) GET(p string, h Handler, o ...RouteOption) { g.Handle(http.MethodGet, p, h, o...) }

// POST registers a group POST route.
func (g *Group) POST(p string, h Handler, o ...RouteOption) { g.Handle(http.MethodPost, p, h, o...) }

// PUT registers a group PUT route.
func (g *Group) PUT(p string, h Handler, o ...RouteOption) { g.Handle(http.MethodPut, p, h, o...) }

// PATCH registers a group PATCH route.
func (g *Group) PATCH(p string, h Handler, o ...RouteOption) { g.Handle(http.MethodPatch, p, h, o...) }

// DELETE registers a group DELETE route.
func (g *Group) DELETE(p string, h Handler, o ...RouteOption) {
	g.Handle(http.MethodDelete, p, h, o...)
}
