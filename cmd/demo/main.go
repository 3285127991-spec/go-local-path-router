// Command demo is a small CLI demonstration of the pathrouter library. It
// registers a fixed set of routes and then either looks up a single
// method/path pair given as arguments or runs a built-in demo table.
//
// Usage:
//
//	go run ./cmd/demo            # run the built-in demo table
//	go run ./cmd/demo GET /users/42
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"pathrouter"
)

func main() {
	r := pathrouter.New()
	for _, rt := range []struct{ method, pattern, handler string }{
		{"GET", "/", "home"},
		{"GET", "/users", "user-list"},
		{"POST", "/users", "user-create"},
		{"GET", "/users/new", "user-new-form"},
		{"GET", "/users/:id", "user-show"},
		{"PUT", "/users/:id", "user-update"},
		{"DELETE", "/users/:id", "user-delete"},
		{"GET", "/files/*path", "file-serve"},
	} {
		if err := r.Add(rt.method, rt.pattern, rt.handler); err != nil {
			fmt.Fprintf(os.Stderr, "register %s %s: %v\n", rt.method, rt.pattern, err)
			os.Exit(1)
		}
	}

	switch len(os.Args) {
	case 1:
		runTable(r)
	case 3:
		lookup(r, strings.ToUpper(os.Args[1]), os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "usage: demo [METHOD PATH]\n")
		os.Exit(2)
	}
}

func runTable(r *pathrouter.Router) {
	fmt.Println("== Matching demo ==")
	for _, d := range [][2]string{
		{"GET", "/"},
		{"GET", "/users"},
		{"POST", "/users"},
		{"GET", "/users/new"},
		{"GET", "/users/42"},
		{"PUT", "/users/42"},
		{"DELETE", "/users/42"},
		{"GET", "/files/css/site.css"},
		{"GET", "/files"},
		{"GET", "/users/"},
		{"POST", "/users/42"},
		{"GET", "/nope"},
	} {
		lookup(r, d[0], d[1])
	}

	fmt.Println("")
	fmt.Println("== Registered routes (stable order) ==")
	for _, rt := range r.Routes() {
		fmt.Printf("%-7s %-14s -> %s\n", rt.Method, rt.Pattern, rt.Handler)
	}
}

func lookup(r *pathrouter.Router, method, path string) {
	handler, params, ok := r.Match(method, path)
	if !ok {
		fmt.Printf("%-7s %-22s -> (no match)\n", method, path)
		return
	}
	fmt.Printf("%-7s %-22s -> %-13s params=%s\n", method, path, handler, formatParams(params))
}

func formatParams(params map[string]string) string {
	if len(params) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s:%q", k, params[k])
	}
	b.WriteByte('}')
	return b.String()
}
