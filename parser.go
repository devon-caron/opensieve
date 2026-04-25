package parser

import (
	"fmt"
	"os"
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
	mu      sync.Mutex
	loaded  map[string]*loadedSpec
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
// cmd against it. cmd may contain pipes; each pipeline segment is
// matched independently in order, and Parse fails fast on the first
// segment that doesn't pass.
func (p *Parser) Parse(toolPath string, cmd string) ParseResult {
	ls, err := p.load(toolPath)
	if err != nil {
		return ParseResult{
			Pass:   false,
			Reason: err,
			Rule:   "Tool path: " + toolPath,
		}
	}

	ruleName := ls.spec.Name

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
func (p *Parser) load(path string) (*loadedSpec, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if ls, ok := p.loaded[path]; ok {
		return ls, nil
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	spec := &tool.ToolSpec{}
	if err := yaml.Unmarshal(bytes, spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	matcher, err := match.FromSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", path, err)
	}

	ls := &loadedSpec{spec: spec, matcher: matcher}
	p.loaded[path] = ls
	return ls, nil
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
