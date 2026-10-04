package pathrouter

import (
	"fmt"
	"strings"
)

type segKind int

const (
	segStatic segKind = iota
	segParam
	segWildcard
)

// segment is one parsed path segment. value holds the literal text for
// static segments and the parameter name for param and wildcard segments.
type segment struct {
	kind  segKind
	value string
}

// splitPath splits an absolute path into segments. "/" has no segments;
// every other path keeps its empty segments, so "/users/" differs from
// "/users".
func splitPath(p string) []string {
	if p == "/" {
		return nil
	}
	return strings.Split(p[1:], "/")
}

// parsePattern validates method and pattern and returns the parsed segments
// together with the parameter and wildcard names in segment order.
func parsePattern(method, pattern string) ([]segment, []string, error) {
	if method == "" {
		return nil, nil, ErrEmptyMethod
	}
	if pattern == "" {
		return nil, nil, ErrEmptyPath
	}
	if pattern[0] != '/' {
		return nil, nil, fmt.Errorf("%w: %q", ErrPathNotAbsolute, pattern)
	}
	raw := splitPath(pattern)
	segs := make([]segment, 0, len(raw))
	var names []string
	seen := make(map[string]bool)
	for i, s := range raw {
		switch {
		case strings.HasPrefix(s, ":"):
			name := s[1:]
			if name == "" {
				return nil, nil, fmt.Errorf("%w: %q", ErrEmptyParamName, pattern)
			}
			if seen[name] {
				return nil, nil, fmt.Errorf("%w: %q in %q", ErrDuplicateParam, name, pattern)
			}
			seen[name] = true
			names = append(names, name)
			segs = append(segs, segment{kind: segParam, value: name})
		case strings.HasPrefix(s, "*"):
			if i != len(raw)-1 {
				return nil, nil, fmt.Errorf("%w: %q", ErrWildcardNotLast, pattern)
			}
			name := s[1:]
			if name == "" {
				return nil, nil, fmt.Errorf("%w: %q", ErrEmptyParamName, pattern)
			}
			if seen[name] {
				return nil, nil, fmt.Errorf("%w: %q in %q", ErrDuplicateParam, name, pattern)
			}
			seen[name] = true
			names = append(names, name)
			segs = append(segs, segment{kind: segWildcard, value: name})
		default:
			segs = append(segs, segment{kind: segStatic, value: s})
		}
	}
	return segs, names, nil
}
