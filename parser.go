package parser

import "github.com/devon-caron/opensieve/tool"

type Parser struct {
	toolSpec *tool.ToolSpec
}

func New(policyPath string) (*Parser, error) {
	panic("unimplemented")
}

type ParseResult struct {
	Pass   bool
	Reason error  // non-nil on rejection
	Rule   string // which policy entry matched (or "" if none)
}

// Parse accepts a command string. Callers who have pre-tokenized argv
// should use ParseArgv instead.
func (p *Parser) Parse(cmd string) ParseResult {
	// Placeholder
	panic("unimplemented")
}

func (p *Parser) ParseArgv(argv []string) ParseResult {
	//Placeholder
	panic("unimplemented")
}
