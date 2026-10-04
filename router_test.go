package router_test

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	router "pathrouter"
)

func mustAdd(t *testing.T, r *router.Router, method, pattern, handler string) {
	t.Helper()
	if err := r.Add(method, pattern, handler); err != nil {
		t.Fatalf("Add(%q, %q) failed: %v", method, pattern, err)
	}
}

func assertMatch(t *testing.T, r *router.Router, method, path, wantHandler string, wantParams map[string]string) {
	t.Helper()
	handler, params, ok := r.Match(method, path)
	if !ok {
		t.Fatalf("Match(%q, %q): no match, want handler %q", method, path, wantHandler)
	}
	if handler != wantHandler {
		t.Fatalf("Match(%q, %q): handler = %q, want %q", method, path, handler, wantHandler)
	}
	if !reflect.DeepEqual(params, wantParams) {
		t.Fatalf("Match(%q, %q): params = %v, want %v", method, path, params, wantParams)
	}
}

func assertNoMatch(t *testing.T, r *router.Router, method, path string) {
	t.Helper()
	if handler, params, ok := r.Match(method, path); ok {
		t.Fatalf("Match(%q, %q): got handler %q params %v, want no match", method, path, handler, params)
	}
}

func TestStaticMatch(t *testing.T) {
	r := router.New()
	mustAdd(t, r, "GET", "/users/new", "users.new")
	mustAdd(t, r, "GET", "/", "root")

	assertMatch(t, r, "GET", "/users/new", "users.new", nil)
	assertMatch(t, r, "GET", "/", "root", nil)
	assertNoMatch(t, r, "GET", "/users/new/extra")
	assertNoMatch(t, r, "GET", "/users")
}

func TestParamExtraction(t *testing.T) {
	r := router.New()
	mustAdd(t, r, "GET", "/users/:id", "users.show")
	mustAdd(t, r, "GET", "/users/:id/posts/:post", "posts.show")

	assertMatch(t, r, "GET", "/users/42", "users.show", map[string]string{"id": "42"})
	assertMatch(t, r, "GET", "/users/42/posts/7", "posts.show",
		map[string]string{"id": "42", "post": "7"})
	assertNoMatch(t, r, "GET", "/users/42/posts")
}

func TestWildcard(t *testing.T) {
	r := router.New()
	mustAdd(t, r, "GET", "/files/*path", "files.serve")

	assertMatch(t, r, "GET", "/files/a.txt", "files.serve", map[string]string{"path": "a.txt"})
	assertMatch(t, r, "GET", "/files/a/b/c.txt", "files.serve", map[string]string{"path": "a/b/c.txt"})
	// A trailing wildcard requires at least one remaining segment.
	assertNoMatch(t, r, "GET", "/files")
}

// Priority must hold regardless of registration order: the wildcard is
// registered first and the static route last, on purpose.
func TestMatchPriority(t *testing.T) {
	r := router.New()
	mustAdd(t, r, "GET", "/users/*rest", "users.wild")
	mustAdd(t, r, "GET", "/users/:id", "users.show")
	mustAdd(t, r, "GET", "/users/new", "users.new")

	assertMatch(t, r, "GET", "/users/new", "users.new", nil)                           // static beats param
	assertMatch(t, r, "GET", "/users/42", "users.show", map[string]string{"id": "42"}) // param beats wildcard
	assertMatch(t, r, "GET", "/users/a/b", "users.wild", map[string]string{"rest": "a/b"})

	// Re-registering in the opposite order must not change the outcome.
	r2 := router.New()
	mustAdd(t, r2, "GET", "/users/new", "users.new")
	mustAdd(t, r2, "GET", "/users/:id", "users.show")
	mustAdd(t, r2, "GET", "/users/*rest", "users.wild")

	assertMatch(t, r2, "GET", "/users/new", "users.new", nil)
	assertMatch(t, r2, "GET", "/users/42", "users.show", map[string]string{"id": "42"})
	assertMatch(t, r2, "GET", "/users/a/b", "users.wild", map[string]string{"rest": "a/b"})
}

// A higher-priority branch that dead-ends must not hide a lower-priority
// route that would have matched: the matcher backtracks.
func TestMatchBacktracking(t *testing.T) {
	r := router.New()
	mustAdd(t, r, "GET", "/users/new", "users.new")
	mustAdd(t, r, "GET", "/users/:id/profile", "users.profile")

	// Static "new" matches the first segment but dead-ends at "profile";
	// the parameter branch must take over.
	assertMatch(t, r, "GET", "/users/new/profile", "users.profile", map[string]string{"id": "new"})
	assertMatch(t, r, "GET", "/users/new", "users.new", nil)
}

func TestMethodIsolation(t *testing.T) {
	r := router.New()
	mustAdd(t, r, "GET", "/users/:id", "users.show")
	mustAdd(t, r, "POST", "/users/:id", "users.update")
	mustAdd(t, r, "PUT", "/users/:id", "users.replace")
	mustAdd(t, r, "DELETE", "/users/:id", "users.delete")

	assertMatch(t, r, "GET", "/users/1", "users.show", map[string]string{"id": "1"})
	assertMatch(t, r, "POST", "/users/1", "users.update", map[string]string{"id": "1"})
	assertMatch(t, r, "PUT", "/users/1", "users.replace", map[string]string{"id": "1"})
	assertMatch(t, r, "DELETE", "/users/1", "users.delete", map[string]string{"id": "1"})
	assertNoMatch(t, r, "PATCH", "/users/1")
	// Methods are matched exactly; no case normalization.
	assertNoMatch(t, r, "get", "/users/1")
}

func TestDuplicateRegistration(t *testing.T) {
	r := router.New()
	mustAdd(t, r, "GET", "/users/:id", "users.show")

	if err := r.Add("GET", "/users/:id", "users.other"); !errors.Is(err, router.ErrDuplicateRoute) {
		t.Fatalf("re-Add same method+pattern: err = %v, want ErrDuplicateRoute", err)
	}
	// Same pattern under another method is fine.
	mustAdd(t, r, "POST", "/users/:id", "users.update")
	// A different pattern resolving to the same trie path conflicts.
	if err := r.Add("GET", "/users/:name", "users.other"); !errors.Is(err, router.ErrRouteConflict) {
		t.Fatalf("Add conflicting param name: err = %v, want ErrRouteConflict", err)
	}
	// Original handler must be intact after the failed registrations.
	assertMatch(t, r, "GET", "/users/9", "users.show", map[string]string{"id": "9"})
}

func TestInvalidPatterns(t *testing.T) {
	r := router.New()
	cases := []struct {
		name    string
		method  string
		pattern string
		want    error
	}{
		{"empty method", "", "/users", router.ErrEmptyMethod},
		{"empty pattern", "GET", "", router.ErrEmptyPattern},
		{"relative pattern", "GET", "users", router.ErrPatternNotAbsolute},
		{"wildcard not terminal", "GET", "/files/*path/more", router.ErrWildcardNotTerminal},
		{"bare wildcard not terminal", "GET", "/files/*/more", router.ErrWildcardNotTerminal},
		{"empty param name", "GET", "/users/:", router.ErrEmptyParamName},
		{"empty wildcard name", "GET", "/files/*", router.ErrEmptyParamName},
		{"duplicate param", "GET", "/users/:id/posts/:id", router.ErrDuplicateParamName},
		{"param clashes with wildcard name", "GET", "/files/:name/*name", router.ErrDuplicateParamName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := r.Add(tc.method, tc.pattern, "h"); !errors.Is(err, tc.want) {
				t.Fatalf("Add(%q, %q): err = %v, want %v", tc.method, tc.pattern, err, tc.want)
			}
		})
	}
	if got := len(r.Routes()); got != 0 {
		t.Fatalf("invalid registrations leaked into router: %d routes", got)
	}
}

func TestRemove(t *testing.T) {
	r := router.New()
	mustAdd(t, r, "GET", "/users/:id", "users.show")
	mustAdd(t, r, "GET", "/users/new", "users.new")

	if !r.Remove("GET", "/users/:id") {
		t.Fatal("Remove existing route: got false, want true")
	}
	assertNoMatch(t, r, "GET", "/users/42")
	// Sibling route must survive the removal and pruning.
	assertMatch(t, r, "GET", "/users/new", "users.new", nil)

	// Removing again, or removing unknown/invalid pairs, reports false.
	if r.Remove("GET", "/users/:id") {
		t.Fatal("Remove twice: got true, want false")
	}
	if r.Remove("GET", "/never/registered") {
		t.Fatal("Remove unknown pattern: got true, want false")
	}
	if r.Remove("POST", "/users/new") {
		t.Fatal("Remove unknown method: got true, want false")
	}
	if r.Remove("", "/users/new") || r.Remove("GET", "not-absolute") {
		t.Fatal("Remove invalid input: got true, want false")
	}

	// The pattern can be re-registered after removal.
	mustAdd(t, r, "GET", "/users/:id", "users.show2")
	assertMatch(t, r, "GET", "/users/42", "users.show2", map[string]string{"id": "42"})
}

func TestRoutesStableOrder(t *testing.T) {
	r := router.New()
	// Registered in deliberately unsorted order.
	mustAdd(t, r, "POST", "/users", "users.create")
	mustAdd(t, r, "GET", "/users/:id", "users.show")
	mustAdd(t, r, "GET", "/files/*path", "files.serve")
	mustAdd(t, r, "DELETE", "/users/:id", "users.delete")
	mustAdd(t, r, "GET", "/users/new", "users.new")

	want := []router.Route{
		{Method: "DELETE", Pattern: "/users/:id", Handler: "users.delete"},
		{Method: "GET", Pattern: "/files/*path", Handler: "files.serve"},
		{Method: "GET", Pattern: "/users/:id", Handler: "users.show"},
		{Method: "GET", Pattern: "/users/new", Handler: "users.new"},
		{Method: "POST", Pattern: "/users", Handler: "users.create"},
	}
	for i := 0; i < 3; i++ {
		if got := r.Routes(); !reflect.DeepEqual(got, want) {
			t.Fatalf("Routes() call %d = %v, want %v", i, got, want)
		}
	}

	r.Remove("GET", "/users/new")
	if got := len(r.Routes()); got != len(want)-1 {
		t.Fatalf("Routes() after Remove: %d entries, want %d", got, len(want)-1)
	}
}

func TestNoMatch(t *testing.T) {
	r := router.New()
	mustAdd(t, r, "GET", "/users/:id", "users.show")

	assertNoMatch(t, r, "GET", "/accounts/1")
	assertNoMatch(t, r, "POST", "/users/1")
	assertNoMatch(t, r, "", "/users/1")
	assertNoMatch(t, r, "GET", "")
	assertNoMatch(t, r, "GET", "users/1")
}

// "/users" and "/users/" are different paths: a trailing slash adds an
// empty final segment and neither matches the other's pattern.
func TestTrailingSlash(t *testing.T) {
	r := router.New()
	mustAdd(t, r, "GET", "/users", "users.list")
	mustAdd(t, r, "GET", "/users/", "users.list.slash")

	assertMatch(t, r, "GET", "/users", "users.list", nil)
	assertMatch(t, r, "GET", "/users/", "users.list.slash", nil)

	r2 := router.New()
	mustAdd(t, r2, "GET", "/users/:id", "users.show")
	assertNoMatch(t, r2, "GET", "/users")
	assertMatch(t, r2, "GET", "/users/", "users.show", map[string]string{"id": ""})
}

func TestConcurrentMatchAndWrites(t *testing.T) {
	r := router.New()
	mustAdd(t, r, "GET", "/users/:id", "users.show")
	mustAdd(t, r, "GET", "/users/new", "users.new")
	mustAdd(t, r, "GET", "/files/*path", "files.serve")

	const readers = 8
	const loops = 500

	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < loops; j++ {
				if _, _, ok := r.Match("GET", "/users/new"); !ok {
					t.Error("concurrent Match(/users/new): no match")
					return
				}
				h, p, ok := r.Match("GET", "/users/7")
				if !ok || h != "users.show" || p["id"] != "7" {
					t.Errorf("concurrent Match(/users/7) = %q %v %v", h, p, ok)
					return
				}
				if _, _, ok := r.Match("GET", "/files/a/b"); !ok {
					t.Error("concurrent Match(/files/a/b): no match")
					return
				}
				_ = r.Routes()
			}
		}()
	}
	// Writers must not corrupt the trie while readers match.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < loops; j++ {
				pattern := fmt.Sprintf("/tmp/%d/%d", id, j)
				if err := r.Add("GET", pattern, "tmp"); err != nil {
					t.Errorf("concurrent Add(%q): %v", pattern, err)
					return
				}
				if !r.Remove("GET", pattern) {
					t.Errorf("concurrent Remove(%q): got false", pattern)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	// Structure must still be intact and free of writer leftovers.
	assertMatch(t, r, "GET", "/users/new", "users.new", nil)
	assertMatch(t, r, "GET", "/users/7", "users.show", map[string]string{"id": "7"})
	if got := len(r.Routes()); got != 3 {
		t.Fatalf("Routes() after concurrent writes: %d entries, want 3", got)
	}
}
