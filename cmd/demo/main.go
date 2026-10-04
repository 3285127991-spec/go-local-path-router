// Command demo exercises the pathrouter library from the command line.
//
// Without arguments it runs a scripted demonstration. With arguments it
// treats them as METHOD PATH pairs and matches them against the same
// sample routes:
//
//	go run ./cmd/demo GET /users/42 DELETE /users/7
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	router "pathrouter"
)

func build() *router.Router {
	r := router.New()
	routes := []struct {
		method, pattern, handler string
	}{
		{"GET", "/", "home"},
		{"GET", "/users", "users.list"},
		{"POST", "/users", "users.create"},
		{"GET", "/users/new", "users.newForm"},
		{"GET", "/users/:id", "users.show"},
		{"PUT", "/users/:id", "users.replace"},
		{"DELETE", "/users/:id", "users.destroy"},
		{"GET", "/files/*path", "files.serve"},
	}
	for _, rt := range routes {
		if err := r.Add(rt.method, rt.pattern, rt.handler); err != nil {
			fmt.Fprintf(os.Stderr, "register %s %s: %v\n", rt.method, rt.pattern, err)
			os.Exit(1)
		}
	}
	return r
}

func showMatch(r *router.Router, method, path string) {
	handler, params, ok := r.Match(method, path)
	if !ok {
		fmt.Printf("  %-6s %-22s -> no match\n", method, path)
		return
	}
	if len(params) == 0 {
		fmt.Printf("  %-6s %-22s -> %s\n", method, path, handler)
		return
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s=%q", k, params[k])
	}
	fmt.Printf("  %-6s %-22s -> %s  {%s}\n", method, path, handler, b.String())
}

func main() {
	r := build()

	if len(os.Args) > 1 {
		if len(os.Args)%2 != 1 {
			fmt.Fprintln(os.Stderr, "usage: demo [METHOD PATH]...")
			os.Exit(2)
		}
		for i := 1; i+1 < len(os.Args); i += 2 {
			showMatch(r, strings.ToUpper(os.Args[i]), os.Args[i+1])
		}
		return
	}

	fmt.Println("registered routes (stable order):")
	for _, rt := range r.Routes() {
		fmt.Printf("  %-6s %-14s -> %s\n", rt.Method, rt.Pattern, rt.Handler)
	}

	fmt.Println("\nmatching requests:")
	showMatch(r, "GET", "/")
	showMatch(r, "GET", "/users")
	showMatch(r, "POST", "/users")
	showMatch(r, "GET", "/users/new")     // static beats :id
	showMatch(r, "GET", "/users/42")      // parameter
	showMatch(r, "DELETE", "/users/42")   // same pattern, other method
	showMatch(r, "PATCH", "/users/42")    // unregistered method
	showMatch(r, "GET", "/files/a/b.txt") // wildcard
	showMatch(r, "GET", "/nope")          // no match

	fmt.Println("\nremove DELETE /users/:id:", r.Remove("DELETE", "/users/:id"))
	fmt.Println("remove DELETE /users/:id again:", r.Remove("DELETE", "/users/:id"))
	showMatch(r, "DELETE", "/users/42")
}
