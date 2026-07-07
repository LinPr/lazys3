// Package filter provides the shared case-insensitive substring matcher
// used by the profile, bucket and object lists in place of the bubbles
// default fuzzy matcher (which matches surprising items for path-like
// names).
package filter

import (
	"unicode"

	"charm.land/bubbles/v2/list"
)

// Substring is a list.FilterFunc that matches a case-insensitive substring
// of the item's FilterValue. MatchedIndexes are rune positions within the
// value so delegates can highlight the match.
func Substring(term string, targets []string) []list.Rank {
	needle := lowerRunes(term)
	ranks := make([]list.Rank, 0, len(targets))
	for i, target := range targets {
		idx := runeIndex(lowerRunes(target), needle)
		if idx < 0 {
			continue
		}
		matches := make([]int, len(needle))
		for j := range matches {
			matches[j] = idx + j
		}
		ranks = append(ranks, list.Rank{Index: i, MatchedIndexes: matches})
	}
	return ranks
}

// lowerRunes lowercases per rune so indexes stay aligned with the original
// string's runes.
func lowerRunes(s string) []rune {
	rs := []rune(s)
	for i, r := range rs {
		rs[i] = unicode.ToLower(r)
	}
	return rs
}

// runeIndex returns the first index of needle within haystack, or -1.
func runeIndex(haystack, needle []rune) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		j := 0
		for ; j < len(needle) && haystack[i+j] == needle[j]; j++ {
		}
		if j == len(needle) {
			return i
		}
	}
	return -1
}
