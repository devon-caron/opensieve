package match

import (
	"github.com/bmatcuk/doublestar/v4"
)

// argMatches reports whether the given token matches the compiled
// argument pattern. Returns an error only if the match engine itself
// fails (e.g., a malformed doublestar pattern that escaped load-time
// validation).
func argMatches(token string, a *compiledArg) (bool, error) {
	switch {
	case a.exact != "":
		return token == a.exact, nil
	case a.regex != nil:
		return a.regex.MatchString(token), nil
	case a.path != "":
		return doublestar.Match(a.path, token)
	}
	// Unreachable: load-time validation guarantees exactly one field
	// is populated.
	return false, nil
}

// anyMatches reports whether the token matches any entry in the list.
// The first-matching entry is returned via matched for use in error
// reporting; nil if no entry matched.
func anyMatches(token string, list []*compiledArg) (matched *compiledArg, err error) {
	for _, a := range list {
		ok, err := argMatches(token, a)
		if err != nil {
			return nil, err
		}
		if ok {
			return a, nil
		}
	}
	return nil, nil
}

// validatePathSpec verifies that a doublestar pattern parses. We do
// this at load time by running it against a throwaway string; the
// library only errors on malformed patterns, not on non-matches.
func validatePathSpec(pattern string) error {
	_, err := doublestar.Match(pattern, "")
	return err
}
