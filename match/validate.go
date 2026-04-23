package match

import (
	"fmt"
	"strings"
)

// validateArgs runs the governing entry's rules over the argv tokens
// that follow command/subcommand routing. All violations are
// collected and returned; the matcher does not fail fast.
func validateArgs(e *entry, path []string, args []string) Errors {
	var errs Errors
	cmdPath := strings.Join(path, " ")

	// Per-token allow/deny validation.
	for _, tok := range args {
		if err := validateToken(e, cmdPath, tok); err != nil {
			errs = append(errs, err)
		}
	}

	// Required-args validation: every required pattern must be
	// satisfied by at least one token in argv.
	for _, req := range e.required {
		if !requiredSatisfied(args, req) {
			errs = append(errs, &Error{
				Code:    ErrMissingRequired,
				Command: cmdPath,
				Pattern: req.humanPattern(),
				Msg: fmt.Sprintf(
					"required argument %q not present",
					req.humanPattern()),
			})
		}
	}

	return errs
}

// validateToken checks a single token against the entry's allow/deny
// lists, per the entry's Mode. Returns nil if the token is accepted.
func validateToken(e *entry, cmdPath, tok string) *Error {
	switch e.mode {
	case "whitelist":
		return validateWhitelist(e, cmdPath, tok)
	case "blacklist":
		return validateBlacklist(e, cmdPath, tok)
	default:
		// Unknown mode is a policy bug. Fail closed: reject.
		return &Error{
			Code:    ErrArgNotAllowed,
			Command: cmdPath,
			Token:   tok,
			Msg: fmt.Sprintf(
				"command has unknown mode %q; rejecting by default",
				e.mode),
		}
	}
}

func validateWhitelist(e *entry, cmdPath, tok string) *Error {
	matched, err := anyMatches(tok, e.allowed)
	if err != nil {
		return &Error{
			Code:    ErrArgNotAllowed,
			Command: cmdPath,
			Token:   tok,
			Msg:     "internal pattern match error: " + err.Error(),
		}
	}
	if matched == nil {
		return &Error{
			Code:    ErrArgNotAllowed,
			Command: cmdPath,
			Token:   tok,
			Msg:     "argument is not in the allowed list",
		}
	}
	return nil
}

func validateBlacklist(e *entry, cmdPath, tok string) *Error {
	matched, err := anyMatches(tok, e.disallowed)
	if err != nil {
		return &Error{
			Code:    ErrArgDenied,
			Command: cmdPath,
			Token:   tok,
			Msg:     "internal pattern match error: " + err.Error(),
		}
	}
	if matched != nil {
		return &Error{
			Code:    ErrArgDenied,
			Command: cmdPath,
			Token:   tok,
			Pattern: matched.humanPattern(),
			Msg:     fmt.Sprintf("argument is denied by pattern %q", matched.humanPattern()),
		}
	}
	return nil
}

// requiredSatisfied reports whether at least one token in argv
// matches the required pattern. Per the decision that RequiredArgs is
// a simple contains check, no ordering or positional requirement is
// enforced.
func requiredSatisfied(argv []string, req *compiledArg) bool {
	for _, tok := range argv {
		ok, err := argMatches(tok, req)
		if err != nil {
			// Pattern errors are treated as unsatisfied; the load-time
			// validation should have caught them.
			continue
		}
		if ok {
			return true
		}
	}
	return false
}
