package main

import (
	"fmt"
	"strings"
)

// toStrings creates a []string from any number of stringers.
// It is useful for succinctly creating slices that can be passed to strings.Join().
func toStrings(in ...fmt.Stringer) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = v.String()
	}
	return out
}

// joinStringers is a convenience wrapper for using toStrings() with strings.Join().
func joinStringers(sep string, elems ...fmt.Stringer) string {
	return strings.Join(toStrings(elems...), sep)
}

// enumTag comma-joins stringer values as Kong expects for struct field `enum:` tags.
func enumTag(elems ...fmt.Stringer) string {
	return joinStringers(",", elems...)
}
