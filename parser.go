package opensieve

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/devon-caron/opensieve/lex"
	"github.com/devon-caron/opensieve/match"
	"github.com/devon-caron/opensieve/tool"
	"gopkg.in/yaml.v3"
)

// Parser loads YAML tool specs, compiles them into matchers, and
// validates command strings against them.
//
// Compiled matchers are cached by file path so that repeated Parse
// calls for the same tool reuse the built Index instead of re-reading
// the YAML and re-compiling every regex/path pattern.
type Parser struct {
	mu     sync.Mutex
	loaded map[string]*loadedSpec
}

// loadedSpec bundles a parsed ToolSpec with the Matcher built from it.
// Both are kept so callers can still get at the spec metadata (e.g.,
// the rule name) after a match decision.
type loadedSpec struct {
	spec    *tool.ToolSpec
	matcher *match.Matcher
}

func New() (*Parser, error) {
	return &Parser{loaded: make(map[string]*loadedSpec)}, nil
}

// ParseResult is the outcome of validating a single command string.
//
// Pass is true iff every pipeline segment matched the policy. Reason
// is non-nil on failure: it is either a load-time error from the YAML
// file/lexer or the matcher's *match.Error / match.Errors describing
// the violations. Rule names the policy that gated the decision so
// callers can audit which spec was in force.
type ParseResult struct {
	Pass   bool
	Reason error
	Rule   string
}

// Parse loads (or reuses cached) the spec at toolPath and validates
// the candidate invocation, given as a base command name and an argv
// slice, against it. argv is the list of arguments that follow base,
// in order, with each element representing one already-tokenized word
// (no shell quoting required from the caller).
//
// Internally, base and argv are reassembled into a single command
// string and fed through the existing lexer; quoting is applied to any
// element containing whitespace, the pipe operator, or a quote
// character so that each element survives tokenization as a single
// token. Elements containing characters that the lexer forbids outside
// quotes (e.g. $, *, redirection operators) will be rejected at the
// lex stage; this preserves the policy that those characters are not
// permitted inputs.
//
// argv may include a literal "|" element to express a pipeline; each
// pipeline segment is matched independently in order, and Parse fails
// fast on the first segment that doesn't pass.
func (p *Parser) Parse(toolData string, base string, argv []string) ParseResult {
	ls, err := p.load([]byte(toolData))
	if err != nil {
		return ParseResult{
			Pass:   false,
			Reason: err,
			Rule:   "Tool data: " + toolData,
		}
	}

	ruleName := ls.spec.Name

	cmd := JoinArgv(base, argv)

	tokens, err := lex.Tokenize(cmd)
	if err != nil {
		return ParseResult{
			Pass:   false,
			Reason: err,
			Rule:   ruleName,
		}
	}

	for _, seg := range splitSegments(cmd, tokens) {
		if _, err := ls.matcher.Match(seg); err != nil {
			return ParseResult{
				Pass:   false,
				Reason: err,
				Rule:   ruleName,
			}
		}
	}

	return ParseResult{
		Pass:   true,
		Reason: nil,
		Rule:   ruleName,
	}
}

// load reads, parses, and compiles the YAML at path, caching the
// result. Subsequent calls for the same path return the cached entry
// without touching the file system or rebuilding the Index.
func (p *Parser) load(toolData []byte) (*loadedSpec, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	spec := &tool.ToolSpec{}
	if err := yaml.Unmarshal(toolData, spec); err != nil {
		return nil, fmt.Errorf("parse tool data: %w", err)
	}

	matcher, err := match.FromSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("compile tool data: %w", err)
	}

	ls := &loadedSpec{spec: spec, matcher: matcher}
	p.loaded[string(toolData)] = ls
	return ls, nil
}

// JoinArgv reconstructs a command string from a base command name and
// an argv slice for consumption by the lexer. Elements that would not
// survive tokenization as a single unquoted word are wrapped in quotes
// so that each argv element comes out the other side of lex.Tokenize
// as exactly one TokWord.
//
// JoinArgv and SeparateCommand are exposed so that callers — and other
// projects that integrate with opensieve — can move between the two
// representations through the same canonical implementation that
// Parser.Parse uses internally.
//
// A bare "|" element is the pipeline-boundary marker (see quoteArg).
// When both base and argv are empty, the result is the empty string so
// that lex.Tokenize surfaces ErrEmptyInput rather than emitting a
// quoted-empty-string token.
//
// Deprecated: use JoinCommands for pipelines support. JoinArgv still
// works for single-segment commands.
func JoinArgv(base string, argv []string) string {
	if base == "" && len(argv) == 0 {
		return ""
	}
	parts := make([]string, 0, 1+len(argv))
	parts = append(parts, quoteArg(base))
	for _, a := range argv {
		parts = append(parts, quoteArg(a))
	}
	return strings.Join(parts, " ")
}

// SeparateCommand parses a command string into a base command name and
// the argv slice that follows it, using the canonical lexer. It is the
// inverse of JoinArgv: for any (base, argv) whose JoinArgv result
// tokenizes cleanly, SeparateCommand(JoinArgv(base, argv)) returns the
// original (base, argv).
//
// SeparateCommand handles a single command segment only. If cmd
// contains a pipe operator, it returns ErrPipeInSingleCommand;
// callers with pipelines should use SeparateCommands instead.
// Lexer errors (forbidden chars, unterminated quotes, empty input, etc.)
// are returned verbatim.
//
// Deprecated: use SeparateCommands for pipelines support. SeparateCommand
// still works for single-segment commands.
func SeparateCommand(cmd string) (base string, argv []string, err error) {
	tokens, err := lex.Tokenize(cmd)
	if err != nil {
		return "", nil, err
	}
	words := make([]string, 0, len(tokens))
	for _, t := range tokens {
		switch t.Kind {
		case lex.TokWord:
			words = append(words, t.Value)
		case lex.TokPipe:
			return "", nil, ErrPipeInSingleCommand
		case lex.TokEOF:
			// terminator, ignore
		}
	}
	// lex.Tokenize guarantees ErrEmptyInput on whitespace-only input,
	// so words is always non-empty here when err is nil.
	return words[0], words[1:], nil
}

// ErrPipeInSingleCommand is returned by SeparateCommand when its input
// contains a pipe operator. SeparateCommand handles a single segment
// only; callers expecting pipelines should use SeparateCommands instead.
var ErrPipeInSingleCommand = errors.New(
	"opensieve: SeparateCommand received a pipeline; use SeparateCommands " +
		"or split into segments first")

// CommandSet holds the parsed result of a command string that may contain
// one or more pipeline segments. Each segment has its own base command and
// arguments. The Pipes slice records the positions of pipe operators between
// segments (len(Pipes) == len(Segments) - 1 for valid pipelines).
//
// For a single-segment command (no pipes), Segments has one entry and
// Pipes is nil/empty.
//
// CommandSet is the inverse of JoinCommands: for any CommandSet whose
// JoinCommands result tokenizes cleanly, SeparateCommands(JoinCommands(cs))
// returns the original CommandSet.
type CommandSet struct {
	// Segments holds each command segment (one per pipeline stage).
	// Each segment has a base command and its arguments.
	Segments []CommandSegment

	// Pipes records the number of pipe operators in the command.
	// For n segments, there are n-1 pipes. This field is provided
	// for convenience so callers don't need to derive it from Segments.
	Pipes int
}

// CommandSegment represents a single command within a pipeline.
// It has a base command name and a list of arguments.
type CommandSegment struct {
	// Base is the command name (e.g., "ls", "git").
	Base string

	// Argv is the list of arguments following the base command.
	Argv []string
}

// SeparateCommands parses a command string into a CommandSet, supporting
// pipelines. It is the inverse of JoinCommands: for any CommandSet whose
// JoinCommands result tokenizes cleanly, SeparateCommands(JoinCommands(cs))
// returns the original CommandSet.
//
// Unlike SeparateCommand (which handles only single-segment commands),
// SeparateCommands correctly parses multi-segment pipelines by splitting
// on pipe operators and returning a CommandSet with one segment per pipeline
// stage.
//
// Lexer errors (forbidden chars, unterminated quotes, empty input, etc.)
// are returned verbatim.
func SeparateCommands(cmd string) (CommandSet, error) {
	tokens, err := lex.Tokenize(cmd)
	if err != nil {
		return CommandSet{}, err
	}

	// Count pipes and collect word tokens per segment.
	var segments []CommandSegment
	var currentWords []string
	pipeCount := 0

	for _, t := range tokens {
		switch t.Kind {
		case lex.TokWord:
			currentWords = append(currentWords, t.Value)
		case lex.TokPipe:
			// Flush current segment.
			if len(currentWords) > 0 {
				segments = append(segments, CommandSegment{
					Base: currentWords[0],
					Argv: currentWords[1:],
				})
			}
			currentWords = nil
			pipeCount++
		case lex.TokEOF:
			// Flush final segment.
			if len(currentWords) > 0 {
				segments = append(segments, CommandSegment{
					Base: currentWords[0],
					Argv: currentWords[1:],
				})
			}
		}
	}

	if len(segments) == 0 {
		return CommandSet{}, errors.New("opensieve: no commands found")
	}

	return CommandSet{
		Segments: segments,
		Pipes:    pipeCount,
	}, nil
}

// JoinCommands converts a CommandSet back into a command string.
// It is the inverse of SeparateCommands: for any CommandSet whose
// JoinCommands result tokenizes cleanly, SeparateCommands(JoinCommands(cs))
// returns the original CommandSet.
//
// Each segment's base and argv are joined using the same quoting logic
// as JoinArgv, and segments are separated by pipe operators.
func JoinCommands(cs CommandSet) string {
	if len(cs.Segments) == 0 {
		return ""
	}

	var parts []string
	for i, seg := range cs.Segments {
		if i > 0 {
			parts = append(parts, "|")
		}
		// Build this segment's command string.
		segParts := make([]string, 0, 1+len(seg.Argv))
		segParts = append(segParts, quoteArg(seg.Base))
		for _, a := range seg.Argv {
			segParts = append(segParts, quoteArg(a))
		}
		parts = append(parts, strings.Join(segParts, " "))
	}

	return strings.Join(parts, " ")
}

// ParseCommandSet validates a CommandSet against the policy loaded from
// toolData. Each segment in the CommandSet is matched independently in
// order, and the result fails fast on the first segment that doesn't pass.
//
// This is the preferred entry point when you already have a CommandSet
// from SeparateCommands, as it avoids the round-trip of converting back
// to a command string and then re-parsing it.
func (p *Parser) ParseCommandSet(toolData string, cs CommandSet) ParseResult {
	if len(cs.Segments) == 0 {
		return ParseResult{
			Pass:   false,
			Reason: errors.New("opensieve: empty CommandSet"),
			Rule:   toolData,
		}
	}

	ls, err := p.load([]byte(toolData))
	if err != nil {
		return ParseResult{
			Pass:   false,
			Reason: err,
			Rule:   "Tool data: " + toolData,
		}
	}

	ruleName := ls.spec.Name

	for _, seg := range cs.Segments {
		cmd := JoinArgv(seg.Base, seg.Argv)

		tokens, err := lex.Tokenize(cmd)
		if err != nil {
			return ParseResult{
				Pass:   false,
				Reason: err,
				Rule:   ruleName,
			}
		}

		if _, err := ls.matcher.Match(match.Segment{Tokens: tokens}); err != nil {
			return ParseResult{
				Pass:   false,
				Reason: err,
				Rule:   ruleName,
			}
		}
	}

	return ParseResult{
		Pass:   true,
		Reason: nil,
		Rule:   ruleName,
	}
}

// quoteArg wraps s in quotes whenever any of its characters would not
// survive the lexer as part of a single unquoted word. The intent is
// "one argv element in, one TokWord out" so callers don't have to know
// the lexer's character classes.
//
// A bare "|" element is the pipeline-boundary marker and is emitted
// unquoted so that the lexer produces a TokPipe token; this is how
// callers express pipelines through the argv API. Any element that
// contains "|" mixed with other content is quoted instead, so a single
// argv element never crosses a segment boundary.
//
// The lexer treats single and double quotes as equivalent literal
// delimiters with no escape processing. quoteArg therefore prefers
// double quotes; falls back to single quotes when s contains a literal
// double quote; and falls back to double quotes when s contains both,
// which is unrepresentable through the current lexer's no-escape
// contract — Tokenize will surface ErrUnterminatedQuote in that case,
// which is the expected failure mode rather than silent corruption.
// Likewise, elements containing newlines or other always-forbidden
// control characters cannot round-trip through any quoting and will
// be rejected by Tokenize with ErrForbiddenChar.
//
// An empty string is emitted as "" so that empty argv elements survive
// as zero-value tokens rather than being elided by the joiner.
func quoteArg(s string) string {
	if s == "" {
		return `""`
	}
	if s == "|" {
		return s
	}
	needsQuote := false
	hasDouble := false
	hasSingle := false
	for _, r := range s {
		switch r {
		case '"':
			needsQuote = true
			hasDouble = true
		case '\'':
			needsQuote = true
			hasSingle = true
		default:
			if !isUnquotedSafe(r) {
				needsQuote = true
			}
		}
	}
	if !needsQuote {
		return s
	}
	if !hasDouble {
		return `"` + s + `"`
	}
	if !hasSingle {
		return "'" + s + "'"
	}
	return `"` + s + `"`
}

// isUnquotedSafe reports whether r is permitted inside an unquoted
// word by the lexer. It mirrors lex/chars.go's isWordChar; if that
// set ever widens or narrows there, this must move in lockstep or
// joinArgv will start under- or over-quoting.
func isUnquotedSafe(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	}
	switch r {
	case '-', '_', '.', '/', '=', '+', ':', ',', '@', '%':
		return true
	}
	return false
}

// splitSegments groups tokens into match.Segment values at TokPipe
// boundaries. The trailing TokEOF closes the final segment. Each
// segment's Raw is the corresponding slice of the original input,
// trimmed of surrounding whitespace, so downstream consumers can
// render carets that align with what the user typed.
//
// Empty segments (from `||` or a leading/trailing pipe) are emitted
// with an empty Tokens slice; the matcher rejects those as
// ErrEmptySegment so the failure surfaces with full provenance.
func splitSegments(input string, tokens []lex.Token) []match.Segment {
	var (
		segs     []match.Segment
		current  []lex.Token
		segStart int
	)

	flush := func(end int) {
		var raw string
		if end > segStart {
			raw = strings.TrimSpace(input[segStart:end])
		}
		segs = append(segs, match.Segment{Tokens: current, Raw: raw})
		current = nil
	}

	for _, tok := range tokens {
		switch tok.Kind {
		case lex.TokPipe:
			flush(tok.Pos)
			segStart = tok.Pos + 1
		case lex.TokEOF:
			flush(tok.Pos)
		case lex.TokWord:
			current = append(current, tok)
		}
	}

	return segs
}
