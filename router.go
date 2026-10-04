// Package router implements a local, in-memory HTTP-style path router.
//
// Routes are registered per HTTP method with path patterns built from
// static segments (/users/new), single-segment parameters (/users/:id)
// and a trailing wildcard (/files/*path). Matching priority is always
// static > parameter > wildcard, independent of registration order.
//
// The package is a pure library: it does not open sockets, touch the
// disk, or depend on any third-party service.
package router

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Sentinel errors returned by Add. Use errors.Is to match them.
var (
	ErrEmptyMethod         = errors.New("router: method must not be empty")
	ErrEmptyPattern        = errors.New("router: pattern must not be empty")
	ErrPatternNotAbsolute  = errors.New("router: pattern must start with '/'")
	ErrDuplicateRoute      = errors.New("router: route already registered for method and pattern")
	ErrRouteConflict       = errors.New("router: pattern conflicts with an existing route")
	ErrWildcardNotTerminal = errors.New("router: wildcard segment must be the last segment")
	ErrEmptyParamName      = errors.New("router: parameter or wildcard name must not be empty")
	ErrDuplicateParamName  = errors.New("router: duplicate parameter name in pattern")
)

// Route describes one registered route.
type Route struct {
	Method  string
	Pattern string
	Handler string
}

type segKind int

const (
	segStatic segKind = iota
	segParam
	segWild
)

type segment struct {
	kind segKind
	name string // literal value for segStatic, parameter name for segParam/segWild
}

type node struct {
	static    map[string]*node
	param     *node
	paramName string
	wild      *node
	wildName  string
	handler   string
	has       bool // whether handler is set
}

func (n *node) childless() bool {
	return len(n.static) == 0 && n.param == nil && n.wild == nil && !n.has
}

// Router maps method+pattern pairs to handler identifiers.
// The zero value is not usable; create one with New.
type Router struct {
	mu     sync.RWMutex
	roots  map[string]*node // method -> trie root
	routes map[string]Route // method+"\x00"+pattern -> route
}

// New returns an empty Router ready for use.
func New() *Router {
	return &Router{
		roots:  make(map[string]*node),
		routes: make(map[string]Route),
	}
}

// splitPath converts an absolute path into segments. The root path "/"
// yields zero segments; a trailing slash yields a final empty segment,
// so "/users" and "/users/" are distinct.
func splitPath(p string) []string {
	if p == "/" {
		return nil
	}
	return strings.Split(p[1:], "/")
}

// parsePattern validates a pattern and splits it into typed segments.
func parsePattern(pattern string) ([]segment, error) {
	if pattern == "" {
		return nil, ErrEmptyPattern
	}
	if pattern[0] != '/' {
		return nil, fmt.Errorf("%w: %q", ErrPatternNotAbsolute, pattern)
	}
	if pattern == "/" {
		return nil, nil
	}
	parts := strings.Split(pattern[1:], "/")
	segs := make([]segment, len(parts))
	names := make(map[string]bool, len(parts))
	for i, part := range parts {
		switch {
		case strings.HasPrefix(part, "*"):
			if i != len(parts)-1 {
				return nil, fmt.Errorf("%w: %q", ErrWildcardNotTerminal, pattern)
			}
			name := part[1:]
			if name == "" {
				return nil, fmt.Errorf("%w: wildcard in %q", ErrEmptyParamName, pattern)
			}
			if names[name] {
				return nil, fmt.Errorf("%w: %q in %q", ErrDuplicateParamName, name, pattern)
			}
			names[name] = true
			segs[i] = segment{kind: segWild, name: name}
		case strings.HasPrefix(part, ":"):
			name := part[1:]
			if name == "" {
				return nil, fmt.Errorf("%w: %q", ErrEmptyParamName, pattern)
			}
			if names[name] {
				return nil, fmt.Errorf("%w: %q in %q", ErrDuplicateParamName, name, pattern)
			}
			names[name] = true
			segs[i] = segment{kind: segParam, name: name}
		default:
			segs[i] = segment{kind: segStatic, name: part}
		}
	}
	return segs, nil
}

func routeKey(method, pattern string) string {
	return method + "\x00" + pattern
}

// Add registers handler for the given method and pattern.
//
// Method must be non-empty; any non-empty string (GET, POST, ...) is
// accepted and matched exactly (no case normalization). The pattern must
// start with '/'. Registering the same method+pattern twice fails with
// ErrDuplicateRoute; a different pattern that resolves to the same trie
// path (e.g. /users/:id vs /users/:name) fails with ErrRouteConflict.
func (r *Router) Add(method, pattern, handler string) error {
	if method == "" {
		return ErrEmptyMethod
	}
	segs, err := parsePattern(pattern)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, dup := r.routes[routeKey(method, pattern)]; dup {
		return fmt.Errorf("%w: %s %s", ErrDuplicateRoute, method, pattern)
	}

	root, ok := r.roots[method]
	if !ok {
		root = &node{}
		r.roots[method] = root
	}
	cur := root
	for _, seg := range segs {
		switch seg.kind {
		case segStatic:
			if cur.static == nil {
				cur.static = make(map[string]*node)
			}
			next, ok := cur.static[seg.name]
			if !ok {
				next = &node{}
				cur.static[seg.name] = next
			}
			cur = next
		case segParam:
			if cur.param == nil {
				cur.param = &node{}
				cur.paramName = seg.name
			} else if cur.paramName != seg.name {
				return fmt.Errorf("%w: parameter %q clashes with %q", ErrRouteConflict, seg.name, cur.paramName)
			}
			cur = cur.param
		case segWild:
			if cur.wild == nil {
				cur.wild = &node{}
				cur.wildName = seg.name
			} else if cur.wildName != seg.name {
				return fmt.Errorf("%w: wildcard %q clashes with %q", ErrRouteConflict, seg.name, cur.wildName)
			}
			cur = cur.wild
		}
	}
	if cur.has {
		return fmt.Errorf("%w: %s %s", ErrRouteConflict, method, pattern)
	}
	cur.handler = handler
	cur.has = true
	r.routes[routeKey(method, pattern)] = Route{Method: method, Pattern: pattern, Handler: handler}
	return nil
}

// Remove deletes the route for the exact method+pattern pair. It reports
// whether a route was actually removed; unknown or invalid pairs return
// false.
func (r *Router) Remove(method, pattern string) bool {
	if method == "" {
		return false
	}
	segs, err := parsePattern(pattern)
	if err != nil {
		return false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := routeKey(method, pattern)
	if _, ok := r.routes[key]; !ok {
		return false
	}
	root := r.roots[method]

	// Walk down, remembering the path taken for later pruning.
	type step struct {
		from *node
		seg  segment
	}
	path := make([]step, 0, len(segs))
	cur := root
	for _, seg := range segs {
		path = append(path, step{from: cur, seg: seg})
		switch seg.kind {
		case segStatic:
			cur = cur.static[seg.name]
		case segParam:
			cur = cur.param
		case segWild:
			cur = cur.wild
		}
	}
	cur.handler = ""
	cur.has = false
	delete(r.routes, key)

	// Prune nodes that no longer carry any route.
	for i := len(path) - 1; i >= 0 && cur.childless(); i-- {
		parent, seg := path[i].from, path[i].seg
		switch seg.kind {
		case segStatic:
			delete(parent.static, seg.name)
		case segParam:
			parent.param = nil
			parent.paramName = ""
		case segWild:
			parent.wild = nil
			parent.wildName = ""
		}
		cur = parent
	}
	if root.childless() {
		delete(r.roots, method)
	}
	return true
}

// Match finds the handler for an actual request path. It returns the
// handler identifier, the extracted parameters (nil when the matched
// pattern has none), and whether a route matched.
//
// Priority at every segment is static > parameter > wildcard, regardless
// of registration order. A wildcard matches one or more remaining
// segments and captures them joined with '/'.
func (r *Router) Match(method, path string) (handler string, params map[string]string, ok bool) {
	if method == "" || path == "" || path[0] != '/' {
		return "", nil, false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	root := r.roots[method]
	if root == nil {
		return "", nil, false
	}
	handler, ok = matchNode(root, splitPath(path), 0, &params)
	if !ok {
		return "", nil, false
	}
	return handler, params, true
}

// matchNode walks the trie depth-first, trying branches in priority
// order (static, then parameter, then wildcard) and backtracking when a
// higher-priority branch turns out to be a dead end. Parameters are
// written only while unwinding a successful match, so failed branches
// never pollute the params map.
func matchNode(cur *node, segs []string, i int, params *map[string]string) (string, bool) {
	if i == len(segs) {
		if cur.has {
			return cur.handler, true
		}
		return "", false
	}
	if next, hit := cur.static[segs[i]]; hit {
		if handler, ok := matchNode(next, segs, i+1, params); ok {
			return handler, true
		}
	}
	if cur.param != nil {
		if handler, ok := matchNode(cur.param, segs, i+1, params); ok {
			setParam(params, cur.paramName, segs[i])
			return handler, true
		}
	}
	if cur.wild != nil && cur.wild.has {
		setParam(params, cur.wildName, strings.Join(segs[i:], "/"))
		return cur.wild.handler, true
	}
	return "", false
}

func setParam(params *map[string]string, name, value string) {
	if *params == nil {
		*params = make(map[string]string)
	}
	(*params)[name] = value
}

// Routes returns the registered routes sorted by method, then pattern.
// The result is a snapshot; mutating it does not affect the router.
func (r *Router) Routes() []Route {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Route, 0, len(r.routes))
	for _, rt := range r.routes {
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Method != out[j].Method {
			return out[i].Method < out[j].Method
		}
		return out[i].Pattern < out[j].Pattern
	})
	return out
}
