package match

import (
	"fmt"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/devon-caron/opensieve/lex"
	"github.com/devon-caron/opensieve/tool"
)

// validateArgs runs the leaf entry's rules over the argv tokens that
// remain after subcommand routing. Under the strict-routing model
// only the leaf (deepest matched entry) ever reaches this function;
// intermediate levels' allowed/disallowed/required fields are dead
// config. The leaf's own DisallowedArgs has already been unioned
// with every ancestor's SubcommandsConfig.DisallowedSubArgs at build
// time, so a denial declared anywhere in the chain reaches here.
//
// All violations are collected; this stage does not fail fast so the
// caller can show the agent the complete diagnosis in one shot.
func validateArgs(e *entry, path []string, args []lex.Token) Errors {
	var errs Errors
	cmdPath := strings.Join(path, " ")

	for _, tok := range args {
		if err := validateToken(e, cmdPath, tok); err != nil {
			errs = append(errs, err)
		}
	}

	for _, req := range e.required {
		if !requiredSatisfied(args, req) {
			errs = append(errs, &Error{
				Code:    ErrMissingRequired,
				Command: cmdPath,
				Pattern: req.humanPattern(),
				Source:  req.source,
			})
		}
	}

	return errs
}

// validateToken checks a single token against the entry's allow/deny
// lists, dispatching by mode. Returns nil if the token is accepted.
func validateToken(e *entry, cmdPath string, tok lex.Token) *Error {
	switch e.mode {
	case tool.CommandModeWhitelist:
		return validateWhitelist(e, cmdPath, tok)
	case tool.CommandModeBlacklist:
		return validateBlacklist(e, cmdPath, tok)
	default:
		// Unknown mode is a policy bug. Fail closed: reject every arg
		// with a clear explanation of why.
		return &Error{
			Code:    ErrArgNotAllowed,
			Command: cmdPath,
			Token:   tok.Value,
			Pos:     tok.Pos,
			Reason: fmt.Sprintf(
				"command has unknown mode %q; rejecting all arguments by default.",
				e.mode),
		}
	}
}

func validateWhitelist(e *entry, cmdPath string, tok lex.Token) *Error {
	matched, err := anyMatches(tok.Value, e.allowed)
	if err != nil {
		return &Error{
			Code:    ErrArgNotAllowed,
			Command: cmdPath,
			Token:   tok.Value,
			Pos:     tok.Pos,
			Reason:  "internal pattern match error: " + err.Error(),
		}
	}
	if matched == nil {
		return &Error{
			Code:    ErrArgNotAllowed,
			Command: cmdPath,
			Token:   tok.Value,
			Pos:     tok.Pos,
			Allowed: e.allowedPatterns(),
		}
	}
	return nil
}

func validateBlacklist(e *entry, cmdPath string, tok lex.Token) *Error {
	matched, err := anyMatches(tok.Value, e.disallowed)
	if err != nil {
		return &Error{
			Code:    ErrArgDenied,
			Command: cmdPath,
			Token:   tok.Value,
			Pos:     tok.Pos,
			Reason:  "internal pattern match error: " + err.Error(),
		}
	}
	if matched != nil {
		return &Error{
			Code:    ErrArgDenied,
			Command: cmdPath,
			Token:   tok.Value,
			Pos:     tok.Pos,
			Pattern: matched.humanPattern(),
			Source:  matched.source,
		}
	}
	return nil
}

// requiredSatisfied reports whether at least one token in argv
// matches the required pattern. Per the policy model, RequiredArgs
// is a simple "contains" check — no positional or ordering constraint.
func requiredSatisfied(argv []lex.Token, req *compiledArg) bool {
	for _, tok := range argv {
		ok, err := argMatches(tok.Value, req)
		if err != nil {
			// Pattern errors are treated as unsatisfied; load-time
			// validation should have caught them.
			continue
		}
		if ok {
			return true
		}
	}
	return false
}

// argMatches reports whether token matches the compiled argument
// pattern. Returns an error only if the doublestar engine itself
// fails on a malformed path pattern that escaped load-time validation.
func argMatches(token string, a *compiledArg) (bool, error) {
	switch {
	case a.exact != "":
		return token == a.exact, nil
	case a.regex != nil:
		return a.regex.MatchString(token), nil
	case a.path != "":
		return doublestar.Match(a.path, token)
	}
	// Unreachable: load-time validation guarantees one field is set.
	return false, nil
}

// anyMatches reports whether token matches any entry in the list.
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

// validatePathSpec verifies that a doublestar pattern parses by
// running it against an empty string. The library only errors on
// malformed patterns, not on non-matches.
func validatePathSpec(pattern string) error {
	_, err := doublestar.Match(pattern, "")
	return err
}
