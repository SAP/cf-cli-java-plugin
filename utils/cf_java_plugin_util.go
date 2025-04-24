package utils

import (
	"sort"
	"strings"
	"github.com/lithammer/fuzzysearch/fuzzy"
)


type CfJavaPluginUtil interface {
	FindReasonForAccessError(app string) string
	CheckRequiredTools(app string) (bool, error)
	GetAvailablePath(data string, userpath string) (string, error)
	CopyOverCat(args []string, src string, dest string) error
	DeleteRemoteFile(args []string, path string) error
	FindHeapDumpFile(args []string, fullpath string, fspath string) (string, error)
	FindJFRFile(args []string, fullpath string, fspath string) (string, error)
	FindFile(args []string, fullpath string, fspath string, pattern string) (string, error)
	ListFiles(args []string, path string) ([]string, error)
}

// FuzzySearch returns up to `max` words from `words` that are closest in
// Levenshtein distance to `needle`.
func FuzzySearch(needle string, words []string, max int) []string {
	type match struct {
		distance int
		word     string
	}

	matches := make([]match, 0, len(words))
	for _, w := range words {
		matches = append(matches, match{
			distance: fuzzy.LevenshteinDistance(needle, w),
			word:     w,
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].distance < matches[j].distance
	})

	if max > len(matches) {
		max = len(matches)
	}

	results := make([]string, 0, max)
	for i := 0; i < max; i++ {
		results = append(results, matches[i].word)
	}

	return results
}

// "x, y, or z"
func JoinWithOr(a []string) string {
	if len(a) == 0 {
		return ""
	}
	if len(a) == 1 {
		return a[0]
	}
	return strings.Join(a[:len(a) - 1], ", ") + ", or " + a[len(a) - 1]
}