# pathrouter

A small, dependency-free Go library for matching HTTP-style request paths
against registered route patterns. Pure in-memory backend library plus a
CLI demo: no HTTP server, no network listener, no database, no Docker, no
cloud or third-party services.

## Features

- Register routes per HTTP method: any non-empty method string works
  (`GET`, `POST`, `PUT`, `DELETE`, ...), matched exactly.
- Path patterns with three segment kinds:
  - static: `/users/new`
  - single-segment parameter: `/users/:id`
  - trailing wildcard: `/files/*path` (must be the last segment)
- Matching priority is always **static > parameter > wildcard**,
  independent of registration order, with backtracking when a
  higher-priority branch dead-ends.
- `Match` returns the handler identifier plus the extracted parameters.
- `Remove` deletes an exact method+pattern route and reports whether
  anything was removed.
- `Routes` returns a stable, sorted snapshot of all registered routes.
- Safe for concurrent use: many goroutines may `Match` while others
  `Add`/`Remove` (guarded by `sync.RWMutex`; verified with
  `go test -race`).

## Semantics worth knowing

- A pattern must start with `/`. `/` itself is a valid root pattern.
- A wildcard matches **one or more** remaining segments and captures them
  joined with `/`; `/files` does not match `/files/*path`.
- A trailing slash is significant: `/users` and `/` are different
  paths, so `/users/` does not match the pattern `/users` (it would
  match `/users/:id` with `id=""`).
- Methods are case-sensitive; `get` and `GET` are different methods.
- Only segments **starting with** `:` or `*` are special;
  `/files/a*b` is a plain static segment.

## Validation

`Add` rejects (see the exported sentinel errors, match with
`errors.Is`):

| Rejection                              | Error                   |
|----------------------------------------|-------------------------|
| empty method                           | `ErrEmptyMethod`        |
| empty pattern                          | `ErrEmptyPattern`       |
| pattern not starting with `/`          | `ErrPatternNotAbsolute` |
| duplicate method+pattern               | `ErrDuplicateRoute`     |
| same trie path, different param names  | `ErrRouteConflict`      |
| wildcard not the last segment          | `ErrWildcardNotTerminal`|
| empty parameter/wildcard name          | `ErrEmptyParamName`     |
| repeated parameter name in one pattern | `ErrDuplicateParamName` |

## Usage

```go
package main

import (
	"fmt"

	router "pathrouter"
)

func main() {
	r := router.New()

	_ = r.Add("GET", "/users/new", "users.newForm")
	_ = r.Add("GET", "/users/:id", "users.show")
	_ = r.Add("GET", "/files/*path", "files.serve")
	_ = r.Add("POST", "/users/:id", "users.update")

	handler, params, ok := r.Match("GET", "/users/42")
	fmt.Println(handler, params, ok) // users.show map[id:42] true

	handler, params, ok = r.Match("GET", "/files/css/site.css")
	fmt.Println(handler, params, ok) // files.serve map[path:css/site.css] true

	fmt.Println(r.Remove("GET", "/users/:id")) // true
	fmt.Println(r.Remove("GET", "/users/:id")) // false (already gone)

	for _, rt := range r.Routes() {
		fmt.Println(rt.Method, rt.Pattern, "->", rt.Handler)
	}
}
```

## CLI demo

```sh
# scripted demonstration
go run ./cmd/demo

# match your own METHOD PATH pairs against the sample routes
go run ./cmd/demo GET /users/42 DELETE /users/7
```

## Tests

The test suite covers static matching, parameter extraction, wildcards,
matching priority (both registration orders), backtracking, method
isolation, duplicate registration, invalid patterns, `Remove`, stable
`Routes` ordering, unmatched requests, trailing-slash behavior, and
concurrent `Match`/`Add`/`Remove`.

```sh
# run all tests
go test ./...

# verify concurrency safety with the race detector
go test -race ./...

# vet + build
go vet ./... && go build ./...
```

## Layout

```
router.go         library (package router, module pathrouter)
router_test.go    test suite
cmd/demo/main.go  CLI demo
```
