package parser

import "github.com/devon-caron/opensieve/tool"

type Parser struct {
	toolSpecs []*tool.ToolSpec
}

func New(policyPath string) (*Parser, error) {
	panic("unimplemented")
}

type ParseResult struct {
	Pass   bool
	Reason error  // non-nil on rejection
	Rule   string // which policy entry matched (or "" if none)
}

// Parse accepts a command string.
func (p *Parser) Parse(cmd string) ParseResult {
	// Placeholder
	panic("unimplemented")
}
