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

// Itoa converts an integer to its string representation.
// This is a lightweight alternative to strconv.Itoa that avoids the strconv import
// for packages that only need simple integer formatting.
func Itoa(n int) string {
	if n == 0 {
		return "0"
	}

	neg := n < 0
	if neg {
		n = -n
	}

	var buf [20]byte
	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	if neg {
		i--
		buf[i] = '-'
	}

	return string(buf[i:])
}
