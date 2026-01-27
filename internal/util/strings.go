// Package util provides common utility functions used across the codebase.
package util

import (
	"strconv"
	"strings"
)

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
// This is a thin wrapper around strconv.Itoa for convenience.
func Itoa(n int) string {
	return strconv.Itoa(n)
}

// LevenshteinDistance calculates the edit distance between two strings.
// This measures the minimum number of single-character edits (insertions,
// deletions, or substitutions) required to change one string into the other.
func LevenshteinDistance(a, b string) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	// Create two rows for the DP matrix (space optimization)
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)

	// Initialize first row
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}

	// Fill matrix row by row
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			curr[j] = minInt(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}

	return prev[len(b)]
}

// minInt returns the minimum of three integers.
func minInt(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// SuggestSimilar returns items from candidates that are similar to input.
// Uses Levenshtein distance with a threshold based on input length.
// Returns up to maxSuggestions matches, sorted by similarity.
func SuggestSimilar(input string, candidates []string, maxSuggestions int) []string {
	if len(candidates) == 0 || input == "" {
		return nil
	}

	// Threshold: allow roughly 1 edit per 3 chars, minimum 2
	threshold := len(input)/3 + 1
	if threshold < 2 {
		threshold = 2
	}

	type scored struct {
		name     string
		distance int
	}

	var matches []scored
	for _, c := range candidates {
		dist := LevenshteinDistance(strings.ToLower(input), strings.ToLower(c))
		if dist <= threshold {
			matches = append(matches, scored{c, dist})
		}
	}

	// Sort by distance (closest matches first)
	for i := 0; i < len(matches)-1; i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].distance < matches[i].distance {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}

	// Return top N (nil if no matches)
	if len(matches) == 0 {
		return nil
	}

	result := make([]string, 0, maxSuggestions)
	for i := 0; i < len(matches) && i < maxSuggestions; i++ {
		result = append(result, matches[i].name)
	}

	return result
}
