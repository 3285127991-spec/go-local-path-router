// Package pathrouter provides a small, concurrency-safe router that matches
// request paths against registered patterns built from static segments,
// single-segment parameters and a trailing wildcard.
//
// Patterns look like "/users/new", "/users/:id" or "/files/*path".
// Matching priority is always static segment > parameter > wildcard,
// regardless of registration order.
package pathrouter

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Sentinel errors returned by Add. Use errors.Is to test for them.
var (
	ErrEmptyMethod     = errors.New("pathrouter: method must not be empty")
	ErrEmptyPath       = errors.New("pathrouter: path must not be empty")
	ErrPathNotAbsolute = errors.New("pathrouter: path must start with /")
	ErrDuplicateRoute  = errors.New("pathrouter: route is already registered")
	ErrConflictRoute   = errors.New("pathrouter: route conflicts with an existing route")
	ErrWildcardNotLast = errors.New("pathrouter: wildcard must be the last segment")
	ErrEmptyParamName  = errors.New("pathrouter: parameter name must not be empty")
	ErrDuplicateParam  = errors.New("pathrouter: duplicate parameter name in pattern")
)

// Route describes one registered route.
type Route struct {
	Method  string
	Pattern string
	Handler string
}

// Router matches paths against registered method+pattern routes. Create one
// with New. A Router is safe for concurrent use by multiple goroutines.
type Router struct {
	mu      sync.RWMutex
	methods map[string]*node // method -> trie root
}

// New returns an empty Router.
func New() *Router {
	return &Router{methods: make(map[string]*node)}
}

// Add registers handler for the given method and pattern. The method may be
// any non-empty string and is matched case-sensitively. It returns an error
// wrapping one of the package's sentinel errors when the arguments are
// invalid or the route cannot be registered.
func (r *Router) Add(method, pattern, handler string) error {
	segs, names, err := parsePattern(method, pattern)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	root := r.methods[method]
	if root == nil {
		root = &node{}
		r.methods[method] = root
	}
	n := root
	for _, s := range segs {
		switch s.kind {
		case segStatic:
			if n.static == nil {
				n.static = make(map[string]*node)
			}
			child := n.static[s.value]
			if child == nil {
				child = &node{}
				n.static[s.value] = child
			}
			n = child
		case segParam:
			if n.param == nil {
				n.param = &node{}
			}
			n = n.param
		case segWildcard:
			if n.wildcard == nil {
				n.wildcard = &node{}
			}
			n = n.wildcard
		}
	}
	if n.hasHandler {
		if n.pattern == pattern {
			return fmt.Errorf("%w: %s %s", ErrDuplicateRoute, method, pattern)
		}
		return fmt.Errorf("%w: %s %s conflicts with %s", ErrConflictRoute, method, pattern, n.pattern)
	}
	n.hasHandler = true
	n.handler = handler
	n.pattern = pattern
	n.paramNames = names
	return nil
}

// Remove deletes the route registered for the exact method and pattern. It
// reports whether a route was actually removed.
func (r *Router) Remove(method, pattern string) bool {
	segs, _, err := parsePattern(method, pattern)
	if err != nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	root := r.methods[method]
	if root == nil {
		return false
	}
	removed, _ := root.remove(segs, pattern)
	if removed && root.empty() {
		delete(r.methods, method)
	}
	return removed
}

// Match looks up the handler for method and path. On success it returns the
// handler and a freshly allocated map of extracted parameters (possibly
// empty). Paths that are empty or do not start with "/" never match, and
// parameters never capture empty segments, so "/users/" does not match
// "/users/:id".
func (r *Router) Match(method, path string) (handler string, params map[string]string, ok bool) {
	if method == "" || path == "" || path[0] != '/' {
		return "", nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	root := r.methods[method]
	if root == nil {
		return "", nil, false
	}
	values := make([]string, 0, 4)
	h, names, ok := root.match(splitPath(path), &values)
	if !ok {
		return "", nil, false
	}
	params = make(map[string]string, len(names))
	for i, name := range names {
		params[name] = values[i]
	}
	return h, params, true
}

// Routes returns the currently registered routes sorted by method and then
// by pattern, so the result is stable across calls.
func (r *Router) Routes() []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var routes []Route
	for method, root := range r.methods {
		root.collect(method, &routes)
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Method != routes[j].Method {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Pattern < routes[j].Pattern
	})
	return routes
}

// node is one level of a per-method routing trie. Parameter and wildcard
// edges are unique per node; their names live on the terminal node so that
// structurally identical patterns share edges without name clashes.
type node struct {
	static   map[string]*node
	param    *node
	wildcard *node

	hasHandler bool
	handler    string
	pattern    string   // original pattern, set on terminal nodes
	paramNames []string // parameter/wildcard names in segment order
}

func (n *node) empty() bool {
	return !n.hasHandler && len(n.static) == 0 && n.param == nil && n.wildcard == nil
}

// match descends the trie with backtracking, trying static edges before the
// parameter edge and the parameter edge before the wildcard edge. Captured
// segment values are appended to values in descent order; failed branches
// roll their values back.
func (n *node) match(segs []string, values *[]string) (string, []string, bool) {
	if len(segs) == 0 {
		if n.hasHandler {
			return n.handler, n.paramNames, true
		}
		if n.wildcard != nil && n.wildcard.hasHandler {
			*values = append(*values, "")
			return n.wildcard.handler, n.wildcard.paramNames, true
		}
		return "", nil, false
	}
	seg := segs[0]
	if child, ok := n.static[seg]; ok {
		if h, names, ok := child.match(segs[1:], values); ok {
			return h, names, true
		}
	}
	if n.param != nil && seg != "" {
		*values = append(*values, seg)
		if h, names, ok := n.param.match(segs[1:], values); ok {
			return h, names, true
		}
		*values = (*values)[:len(*values)-1]
	}
	if n.wildcard != nil && n.wildcard.hasHandler {
		*values = append(*values, strings.Join(segs, "/"))
		return n.wildcard.handler, n.wildcard.paramNames, true
	}
	return "", nil, false
}

// remove deletes the terminal handler for segs when its stored pattern
// equals pattern, pruning now-empty nodes on the way back.
func (n *node) remove(segs []segment, pattern string) (removed, empty bool) {
	if len(segs) == 0 {
		if !n.hasHandler || n.pattern != pattern {
			return false, n.empty()
		}
		n.hasHandler = false
		n.handler = ""
		n.pattern = ""
		n.paramNames = nil
		return true, n.empty()
	}
	s := segs[0]
	var child *node
	switch s.kind {
	case segStatic:
		child = n.static[s.value]
	case segParam:
		child = n.param
	case segWildcard:
		child = n.wildcard
	}
	if child == nil {
		return false, n.empty()
	}
	removed, childEmpty := child.remove(segs[1:], pattern)
	if childEmpty {
		switch s.kind {
		case segStatic:
			delete(n.static, s.value)
		case segParam:
			n.param = nil
		case segWildcard:
			n.wildcard = nil
		}
	}
	return removed, n.empty()
}

func (n *node) collect(method string, out *[]Route) {
	if n.hasHandler {
		*out = append(*out, Route{Method: method, Pattern: n.pattern, Handler: n.handler})
	}
	for _, child := range n.static {
		child.collect(method, out)
	}
	if n.param != nil {
		n.param.collect(method, out)
	}
	if n.wildcard != nil {
		n.wildcard.collect(method, out)
	}
}
