package http1

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
)

var (
	ErrInvalidPattern = errors.New("invalid pattern")
)

type pattern struct {
	method   string
	host     string
	path     string
	segments []segment
	handler  http.Handler
}

type segment struct {
	isWildcard   bool
	isMulti      bool
	isExactEnd   bool
	literal      string
	wildcardName string
}

type pathValueKey string

func PathValue(req *http.Request, name string) string {
	if m, ok := req.Context().Value(pathValueKey("vars")).(map[string]string); ok {
		return m[name]
	}
	return ""
}

type ServeMux struct {
	patterns []pattern
	mu       sync.RWMutex
}

var DefaultServeMux = &ServeMux{
	patterns: []pattern{},
}

func (s *ServeMux) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	reqPath := req.URL.Path
	if reqPath == "" {
		reqPath = "/"
	}

	cleanedPath := path.Clean(reqPath)
	if reqPath != "/" && strings.HasSuffix(reqPath, "/") && !strings.HasSuffix(cleanedPath, "/") {
		cleanedPath += "/"
	}

	if reqPath != cleanedPath {
		url := *req.URL
		url.Path = cleanedPath
		http.Redirect(rw, req, url.String(), http.StatusMovedPermanently)
		return
	}

	reqSegs := splitAndUnescapePath(reqPath)

	s.mu.RLock()
	patterns := s.patterns
	s.mu.RUnlock()

	var bestMatch *pattern
	var bestVars map[string]string

	for i := range patterns {
		p := &patterns[i]

		if p.host != "" && p.host != host {
			continue
		}

		if p.method != "" && p.method != req.Method {
			if !(p.method == "GET" && req.Method == "HEAD") {
				continue
			}
		}

		matched, vars := matchPathSegments(p.segments, reqSegs)
		if !matched {
			continue
		}

		if bestMatch == nil || isMoreSpecific(p, bestMatch) {
			bestMatch = p
			bestVars = vars
		}
	}

	if bestMatch == nil && !strings.HasSuffix(reqPath, "/") {
		slashPath := reqPath + "/"
		slashSegs := splitAndUnescapePath(slashPath)

		for i := range patterns {
			p := &patterns[i]
			if p.host != "" && p.host != host {
				continue
			}
			if matched, _ := matchPathSegments(p.segments, slashSegs); matched {
				url := *req.URL
				url.Path = slashPath
				http.Redirect(rw, req, url.String(), http.StatusMovedPermanently)
				return
			}
		}
	}

	if bestMatch == nil {
		http.NotFound(rw, req)
		return
	}

	ctx := context.WithValue(req.Context(), pathValueKey("vars"), bestVars)
	bestMatch.handler.ServeHTTP(rw, req.WithContext(ctx))
}

func (s *ServeMux) register(pattern string, handler http.Handler) {
	if pattern == "" {
		panic("invalid pattern")
	}
	if handler == nil {
		panic("nil handler")
	}

	p, err := parsePattern(pattern)
	if err != nil {
		panic(fmt.Sprintf("http: invalid pattern %q: %v", pattern, err))
	}
	p.handler = handler

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.patterns {
		if conflicts(p, existing) {
			panic(fmt.Sprintf("http: pattern %q conflicts with another pattern", pattern))
		}
	}

	s.patterns = append(s.patterns, p)
}

func splitAndUnescapePath(p string) []string {
	if p == "/" || p == "" {
		return []string{}
	}
	rawSegs := strings.Split(strings.TrimPrefix(p, "/"), "/")
	segs := make([]string, len(rawSegs))
	for i, s := range rawSegs {
		if unescaped, err := url.PathUnescape(s); err == nil {
			segs[i] = unescaped
		} else {
			segs[i] = s
		}
	}
	return segs
}

func matchPathSegments(patSegs []segment, reqSegs []string) (bool, map[string]string) {
	vars := make(map[string]string)
	k := 0

	for k < len(patSegs) && k < len(reqSegs) {
		ps := patSegs[k]

		if ps.isExactEnd {
			return k == len(reqSegs), vars
		}
		if ps.isMulti {
			vars[ps.wildcardName] = strings.Join(reqSegs[k:], "/")
			return true, vars
		}
		if ps.isWildcard {
			vars[ps.wildcardName] = reqSegs[k]
		} else if ps.literal != reqSegs[k] {
			return false, nil
		}

		k++
	}

	if k < len(patSegs) && patSegs[k].isExactEnd {
		return k == len(reqSegs), vars
	}

	if k == len(patSegs) && k == len(reqSegs) {
		return true, vars
	}

	return false, nil
}

func conflicts(p1, p2 pattern) bool {
	if !intersects(p1, p2) {
		return false
	}
	p1SubsetOfP2 := isSubset(p1, p2)
	p2SubsetOfP1 := isSubset(p2, p1)

	return !(p1SubsetOfP2 || p2SubsetOfP1)
}

func isMoreSpecific(p1, p2 *pattern) bool {
	if isSubset(*p1, *p2) && !isSubset(*p2, *p1) {
		return true
	}
	if p1.host != "" && p2.host == "" {
		return true
	}
	return false
}

func intersects(p1, p2 pattern) bool {
	if p1.host != "" && p2.host != "" && p1.host != p2.host {
		return false
	}
	if p1.method != "" && p2.method != "" {
		if p1.method != p2.method && !(p1.method == "GET" && p2.method == "HEAD") && !(p1.method == "HEAD" && p2.method == "GET") {
			return false
		}
	}

	k := 0
	for k < len(p1.segments) && k < len(p2.segments) {
		s1, s2 := p1.segments[k], p2.segments[k]
		if s1.isMulti || s2.isMulti {
			return true
		}
		if s1.isExactEnd || s2.isExactEnd {
			return k == len(p1.segments)-1 && k == len(p2.segments)-1
		}
		if !s1.isWildcard && !s2.isWildcard && s1.literal != s2.literal {
			return false
		}
		k++
	}
	return true
}

func isSubset(p1, p2 pattern) bool {
	if p2.host != "" && p1.host == "" {
		return false
	}
	if p2.host != "" && p1.host != p2.host {
		return false
	}
	if p2.method != "" && p1.method == "" {
		return false
	}
	if p2.method != "" && p1.method != p2.method {
		if !(p2.method == "GET" && p1.method == "HEAD") {
			return false
		}
	}

	for i := range p2.segments {
		s2 := p2.segments[i]

		if s2.isMulti {
			return true
		}
		if i >= len(p1.segments) {
			return false
		}
		s1 := p1.segments[i]
		if s1.isMulti {
			return false
		}
		if !s2.isWildcard && s1.isWildcard {
			return false
		}
		if !s2.isWildcard && !s1.isWildcard && s1.literal != s2.literal {
			return false
		}
	}

	return len(p1.segments) == len(p2.segments)
}

func parsePattern(patternStr string) (pattern, error) {
	parsedPattern := pattern{}
	rest := patternStr

	if idx := strings.IndexAny(rest, " \t"); idx != -1 {
		parsedPattern.method = rest[:idx]
		rest = strings.TrimLeft(rest[idx:], " \t")
	}

	slashIdx := strings.IndexByte(rest, '/')
	if slashIdx == -1 {
		return pattern{}, ErrInvalidPattern
	}
	parsedPattern.host = rest[:slashIdx]
	parsedPattern.path = rest[slashIdx:]

	rawSegments := strings.Split(parsedPattern.path[1:], "/")
	for i, raw := range rawSegments {
		isLast := i == len(rawSegments)-1

		if raw == "{$}" {
			if !isLast {
				return pattern{}, ErrInvalidPattern
			}
			parsedPattern.segments = append(parsedPattern.segments, segment{isExactEnd: true})
			continue
		}

		if raw == "" && isLast {
			parsedPattern.segments = append(parsedPattern.segments, segment{
				isWildcard: true,
				isMulti:    true,
			})
			continue
		}

		if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
			name := raw[1 : len(raw)-1]
			isMulti := false

			if strings.HasSuffix(name, "...") {
				if !isLast {
					return pattern{}, ErrInvalidPattern
				}
				isMulti = true
				name = strings.TrimSuffix(name, "...")
			}

			if !isValidGoIdentifier(name) {
				return pattern{}, ErrInvalidPattern
			}

			parsedPattern.segments = append(parsedPattern.segments, segment{
				isWildcard:   true,
				isMulti:      isMulti,
				wildcardName: name,
			})
			continue
		}

		unescaped, err := url.PathUnescape(raw)
		if err != nil {
			return pattern{}, ErrInvalidPattern
		}
		parsedPattern.segments = append(parsedPattern.segments, segment{literal: unescaped})
	}

	return parsedPattern, nil
}

func isValidGoIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	DefaultServeMux.register(pattern, http.HandlerFunc(handler))
}
