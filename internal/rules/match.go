package rules

import "strings"

func matchPattern(pattern, path string) bool {
	if !strings.Contains(pattern, "*") {
		return pattern == path
	}
	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	if len(patternParts) != len(pathParts) {
		return false
	}
	for i := range patternParts {
		if patternParts[i] == "*" {
			if pathParts[i] == "" {
				return false
			}
			continue
		}
		if patternParts[i] != pathParts[i] {
			return false
		}
	}
	return true
}

func better(a, b Rule) bool {
	aExact := !strings.Contains(a.PathPattern, "*")
	bExact := !strings.Contains(b.PathPattern, "*")
	if aExact != bExact {
		return aExact
	}
	if !aExact {
		aLiterals := literalSegments(a.PathPattern)
		bLiterals := literalSegments(b.PathPattern)
		if aLiterals != bLiterals {
			return aLiterals > bLiterals
		}
	}
	if len(a.PathPattern) != len(b.PathPattern) {
		return len(a.PathPattern) > len(b.PathPattern)
	}
	if (a.UpstreamID == "") != (b.UpstreamID == "") {
		return a.UpstreamID != ""
	}
	if !a.CreatedAt.Equal(b.CreatedAt.Time) {
		return a.CreatedAt.Before(b.CreatedAt.Time)
	}
	return a.ID < b.ID
}

func literalSegments(pattern string) int {
	parts := strings.Split(pattern, "/")
	count := 0
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" && parts[i] != "*" {
			count++
		}
	}
	return count
}
