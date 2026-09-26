package search

import (
	"strings"
	"unicode"
)

// Score ranks entries containing the query characters in their typed order.
// A negative score means no match; spaces and dashes do not affect matching.
// Repeated letters in a candidate must be typed before matching can continue.
func Score(query, candidate string) int {
	needle, haystack := normalize(query), normalize(candidate)
	if len(needle) == 0 || len(haystack) == 0 || len(needle) > 128 || len(needle) > len(haystack) {
		return -1
	}
	if string(needle) == string(haystack) {
		return 0
	}

	best := -1
	for start, r := range haystack {
		if r != needle[0] {
			continue
		}
		matched, end := 1, start
		for i := start + 1; i < len(haystack) && matched < len(needle); i++ {
			if haystack[i] == needle[matched] {
				matched++
				end = i
			} else if haystack[i] == haystack[i-1] && haystack[i] == haystack[end] {
				// Do not skip the second d in "Eddie" when searching "Edie".
				break
			}
		}
		if matched != len(needle) {
			continue
		}
		gaps := end - start + 1 - len(needle)
		score := 10 + gaps*8 + start + (len(haystack)-len(needle))/4
		if best < 0 || score < best {
			best = score
		}
	}
	return best
}

func normalize(value string) []rune {
	var result []rune
	for _, r := range strings.ToLower(value) {
		if unicode.IsSpace(r) || unicode.Is(unicode.Pd, r) || r == '\u2212' || r == '\u00ad' {
			continue
		}
		result = append(result, r)
	}
	return result
}
