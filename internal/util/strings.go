// Package util provides common utility functions used across the codebase.
package util

import "strings"

// JoinOrNone joins strings with ", " or returns "(none)" for empty slices.
// This is useful for displaying lists of hosts, tags, or other items where
// an empty list should show a placeholder rather than nothing.
func JoinOrNone(items []string) string {
	return JoinOrDefault(items, "(none)")
}

// JoinOrDefault joins strings with ", " or returns the default value for empty slices.
func JoinOrDefault(items []string, def string) string {
	if len(items) == 0 {
		return def
	}
	return strings.Join(items, ", ")
}

// Pluralize returns singular if count is 1, otherwise plural.
func Pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
