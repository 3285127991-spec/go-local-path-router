package pathrouter_test

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"pathrouter"
)

func mustAdd(t *testing.T, r *pathrouter.Router, method, pattern, handler string) {
	t.Helper()
	if err := r.Add(method, pattern, handler); err != nil {
		t.Fatalf("Add(%q, %q, %q) failed: %v", method, pattern, handler, err)
	}
}

func mustMatch(t *testing.T, r *pathrouter.Router, method, path, wantHandler string, wantParams map[string]string) {
	t.Helper()
	handler, params, ok := r.Match(method, path)
	if !ok {
		t.Fatalf("Match(%q, %q): no match, want handler %q", method, path, wantHandler)
	}
	if handler != wantHandler {
		t.Errorf("Match(%q, %q) handler = %q, want %q", method, path, handler, wantHandler)
	}
	if !reflect.DeepEqual(params, wantParams) {
		t.Errorf("Match(%q, %q) params = %v, want %v", method, path, params, wantParams)
	}
}

func mustNoMatch(t *testing.T, r *pathrouter.Router, method, path string) {
	t.Helper()
	if handler, params, ok := r.Match(method, path); ok {
		t.Errorf("Match(%q, %q) = (%q, %v, true), want no match", method, path, handler, params)
	}
}

func TestStaticMatch(t *testing.T) {
	r := pathrouter.New()
	mustAdd(t, r, "GET", "/", "home")
	mustAdd(t, r, "GET", "/users", "user-list")
	mustAdd(t, r, "GET", "/users/new", "user-new")

	mustMatch(t, r, "GET", "/", "home", map[string]string{})
	mustMatch(t, r, "GET", "/users", "user-list", map[string]string{})
	mustMatch(t, r, "GET", "/users/new", "user-new", map[string]string{})
	mustNoMatch(t, r, "GET", "/users/new/more")
	mustNoMatch(t, r, "GET", "/user")
}

func TestParamExtraction(t *testing.T) {
	r := pathrouter.New()
	mustAdd(t, r, "GET", "/users/:id", "user-show")
	mustAdd(t, r, "GET", "/users/:id/posts/:postID", "post-show")

	mustMatch(t, r, "GET", "/users/42", "user-show", map[string]string{"id": "42"})
	mustMatch(t, r, "GET", "/users/42/posts/7", "post-show",
		map[string]string{"id": "42", "postID": "7"})

	// Each Match returns an independent params map.
	_, p1, _ := r.Match("GET", "/users/1")
	_, p2, _ := r.Match("GET", "/users/2")
	p1["id"] = "mutated"
	if p2["id"] != "2" {
		t.Errorf("params maps share state: p2[id] = %q, want %q", p2["id"], "2")
	}
}

func TestWildcard(t *testing.T) {
	r := pathrouter.New()
	mustAdd(t, r, "GET", "/files/*path", "file-serve")

	mustMatch(t, r, "GET", "/files/a/b/c", "file-serve", map[string]string{"path": "a/b/c"})
	mustMatch(t, r, "GET", "/files/a", "file-serve", map[string]string{"path": "a"})
	// The wildcard also matches zero remaining segments.
	mustMatch(t, r, "GET", "/files", "file-serve", map[string]string{"path": ""})
	mustMatch(t, r, "GET", "/files/", "file-serve", map[string]string{"path": ""})
	mustNoMatch(t, r, "GET", "/other/a")
}

func TestPriorityIndependentOfRegistrationOrder(t *testing.T) {
	r := pathrouter.New()
	// Register in adversarial order: wildcard, then param, then static.
	mustAdd(t, r, "GET", "/users/*rest", "wild")
	mustAdd(t, r, "GET", "/users/:id", "param")
	mustAdd(t, r, "GET", "/users/new", "static")

	mustMatch(t, r, "GET", "/users/new", "static", map[string]string{})
	mustMatch(t, r, "GET", "/users/42", "param", map[string]string{"id": "42"})
	mustMatch(t, r, "GET", "/users/42/posts", "wild", map[string]string{"rest": "42/posts"})
}

func TestStaticBranchBacktracking(t *testing.T) {
	r := pathrouter.New()
	mustAdd(t, r, "GET", "/users/new", "user-new")
	mustAdd(t, r, "GET", "/users/:id/profile", "profile")

	// The static branch "new" matches the first segment but dead-ends, so
	// the matcher must fall back to the parameter branch.
	mustMatch(t, r, "GET", "/users/new/profile", "profile", map[string]string{"id": "new"})
	mustMatch(t, r, "GET", "/users/new", "user-new", map[string]string{})
}

func TestMethodIsolation(t *testing.T) {
	r := pathrouter.New()
	mustAdd(t, r, "GET", "/users/:id", "get-user")
	mustAdd(t, r, "POST", "/users/:id", "post-user")
	mustAdd(t, r, "PUT", "/users/:id", "put-user")
	mustAdd(t, r, "DELETE", "/users/:id", "delete-user")
	mustAdd(t, r, "X-CUSTOM", "/users/:id", "custom-user")

	mustMatch(t, r, "GET", "/users/1", "get-user", map[string]string{"id": "1"})
	mustMatch(t, r, "POST", "/users/1", "post-user", map[string]string{"id": "1"})
	mustMatch(t, r, "PUT", "/users/1", "put-user", map[string]string{"id": "1"})
	mustMatch(t, r, "DELETE", "/users/1", "delete-user", map[string]string{"id": "1"})
	mustMatch(t, r, "X-CUSTOM", "/users/1", "custom-user", map[string]string{"id": "1"})

	mustNoMatch(t, r, "PATCH", "/users/1")
	mustNoMatch(t, r, "get", "/users/1") // methods are case-sensitive
	mustNoMatch(t, r, "", "/users/1")
}

func TestAddRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name            string
		method, pattern string
		want            error
	}{
		{"empty method", "", "/users", pathrouter.ErrEmptyMethod},
		{"empty path", "GET", "", pathrouter.ErrEmptyPath},
		{"relative path", "GET", "users", pathrouter.ErrPathNotAbsolute},
		{"wildcard not last", "GET", "/files/*path/more", pathrouter.ErrWildcardNotLast},
		{"wildcard not last and unnamed", "GET", "/files/*/more", pathrouter.ErrWildcardNotLast},
		{"empty param name", "GET", "/users/:", pathrouter.ErrEmptyParamName},
		{"empty wildcard name", "GET", "/files/*", pathrouter.ErrEmptyParamName},
		{"duplicate param", "GET", "/:id/posts/:id", pathrouter.ErrDuplicateParam},
		{"duplicate param and wildcard", "GET", "/:a/*a", pathrouter.ErrDuplicateParam},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := pathrouter.New()
			err := r.Add(tc.method, tc.pattern, "h")
			if !errors.Is(err, tc.want) {
				t.Fatalf("Add(%q, %q) error = %v, want %v", tc.method, tc.pattern, err, tc.want)
			}
			if len(r.Routes()) != 0 {
				t.Fatalf("rejected route was registered: %v", r.Routes())
			}
		})
	}
}

func TestDuplicateAndConflictingRegistration(t *testing.T) {
	r := pathrouter.New()
	mustAdd(t, r, "GET", "/users/:id", "user-show")

	if err := r.Add("GET", "/users/:id", "other"); !errors.Is(err, pathrouter.ErrDuplicateRoute) {
		t.Errorf("re-adding same method+pattern: error = %v, want %v", err, pathrouter.ErrDuplicateRoute)
	}
	// Same shape but different parameter names share trie edges and conflict.
	if err := r.Add("GET", "/users/:name", "other"); !errors.Is(err, pathrouter.ErrConflictRoute) {
		t.Errorf("adding conflicting pattern: error = %v, want %v", err, pathrouter.ErrConflictRoute)
	}
	// The same pattern under another method is fine.
	mustAdd(t, r, "POST", "/users/:id", "post-user")
	// The original route is untouched.
	mustMatch(t, r, "GET", "/users/9", "user-show", map[string]string{"id": "9"})
}

func TestRemove(t *testing.T) {
	r := pathrouter.New()
	mustAdd(t, r, "GET", "/users", "user-list")
	mustAdd(t, r, "GET", "/users/:id", "user-show")
	mustAdd(t, r, "GET", "/users/:id/profile", "profile")

	if !r.Remove("GET", "/users/:id") {
		t.Fatal("Remove of existing route returned false")
	}
	mustNoMatch(t, r, "GET", "/users/42")
	// Sibling routes below and above the removed node survive.
	mustMatch(t, r, "GET", "/users", "user-list", map[string]string{})
	mustMatch(t, r, "GET", "/users/42/profile", "profile", map[string]string{"id": "42"})

	if r.Remove("GET", "/users/:id") {
		t.Error("Remove of already removed route returned true")
	}
	if r.Remove("GET", "/never/registered") {
		t.Error("Remove of unknown route returned true")
	}
	if r.Remove("POST", "/users") {
		t.Error("Remove with wrong method returned true")
	}
	// Same shape but different parameter name is a different route.
	if r.Remove("GET", "/users/:name/profile") {
		t.Error("Remove with different param name returned true")
	}
	if r.Remove("GET", "not-a-pattern") {
		t.Error("Remove with invalid pattern returned true")
	}
	if got := len(r.Routes()); got != 2 {
		t.Errorf("len(Routes()) = %d, want 2", got)
	}
}

func TestRoutesStableOrder(t *testing.T) {
	r := pathrouter.New()
	for _, rt := range []struct{ method, pattern, handler string }{
		{"POST", "/users", "h5"},
		{"GET", "/users/:id", "h3"},
		{"GET", "/users", "h2"},
		{"DELETE", "/users/:id", "h4"},
		{"GET", "/", "h1"},
	} {
		mustAdd(t, r, rt.method, rt.pattern, rt.handler)
	}

	want := []pathrouter.Route{
		{Method: "DELETE", Pattern: "/users/:id", Handler: "h4"},
		{Method: "GET", Pattern: "/", Handler: "h1"},
		{Method: "GET", Pattern: "/users", Handler: "h2"},
		{Method: "GET", Pattern: "/users/:id", Handler: "h3"},
		{Method: "POST", Pattern: "/users", Handler: "h5"},
	}
	for i := 0; i < 3; i++ {
		if got := r.Routes(); !reflect.DeepEqual(got, want) {
			t.Fatalf("Routes() call %d = %v, want %v", i, got, want)
		}
	}

	r.Remove("GET", "/")
	wantAfterRemove := append([]pathrouter.Route{want[0]}, want[2:]...)
	if got := r.Routes(); !reflect.DeepEqual(got, wantAfterRemove) {
		t.Fatalf("Routes() after Remove = %v, want %v", got, wantAfterRemove)
	}
}

func TestNoMatch(t *testing.T) {
	r := pathrouter.New()
	mustAdd(t, r, "GET", "/users/:id", "user-show")

	mustNoMatch(t, r, "GET", "/users")      // missing segment
	mustNoMatch(t, r, "GET", "/users/1/2")  // extra segment
	mustNoMatch(t, r, "GET", "/accounts/1") // unknown prefix
	mustNoMatch(t, r, "POST", "/users/1")   // unknown method
	mustNoMatch(t, r, "GET", "")            // empty path
	mustNoMatch(t, r, "GET", "users/1")     // relative path
	if _, params, ok := r.Match("GET", "/users"); ok || params != nil {
		t.Errorf("failed Match returned params = %v, ok = %v; want nil, false", params, ok)
	}
}

func TestTrailingSlash(t *testing.T) {
	r := pathrouter.New()
	mustAdd(t, r, "GET", "/users", "user-list")

	// Trailing slashes are significant: "/users/" differs from "/users".
	mustNoMatch(t, r, "GET", "/users/")

	mustAdd(t, r, "GET", "/users/", "user-list-slash")
	mustMatch(t, r, "GET", "/users", "user-list", map[string]string{})
	mustMatch(t, r, "GET", "/users/", "user-list-slash", map[string]string{})

	// Parameters never capture empty segments.
	r2 := pathrouter.New()
	mustAdd(t, r2, "GET", "/users/:id", "user-show")
	mustNoMatch(t, r2, "GET", "/users/")
}

func TestConcurrentUse(t *testing.T) {
	r := pathrouter.New()
	mustAdd(t, r, "GET", "/users/:id", "user-show")
	mustAdd(t, r, "GET", "/users/new", "user-new")
	mustAdd(t, r, "GET", "/files/*path", "file-serve")

	var wg sync.WaitGroup
	// Concurrent readers.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				h, params, ok := r.Match("GET", "/users/42")
				if !ok || h != "user-show" || params["id"] != "42" {
					t.Errorf("Match(/users/42) = (%q, %v, %v)", h, params, ok)
					return
				}
				if _, _, ok := r.Match("GET", "/users/new"); !ok {
					t.Errorf("Match(/users/new) failed")
					return
				}
				_ = r.Routes()
			}
		}()
	}
	// Concurrent writers adding and removing disjoint routes.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				p := fmt.Sprintf("/tmp/%d/%d", id, j)
				if err := r.Add("GET", p, "tmp"); err != nil {
					t.Errorf("Add(%q) failed: %v", p, err)
					return
				}
				if !r.Remove("GET", p) {
					t.Errorf("Remove(%q) returned false", p)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	// The seeded routes are intact after the concurrent churn.
	mustMatch(t, r, "GET", "/users/42", "user-show", map[string]string{"id": "42"})
	if got := len(r.Routes()); got != 3 {
		t.Errorf("len(Routes()) = %d, want 3", got)
	}
}
