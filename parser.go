package parser

import (
	"fmt"
	"os"

	"github.com/devon-caron/opensieve/lex"
	"github.com/devon-caron/opensieve/tool"
	"gopkg.in/yaml.v3"
)

type Parser struct {
	toolSpecs []*tool.ToolSpec
}

func New() (*Parser, error) {
	return &Parser{}, nil
}

type ParseResult struct {
	Pass   bool
	Reason error  // non-nil on rejection
	Rule   string // which policy entry matched (or "" if none)
}

// Parse accepts a command string.
func (p *Parser) Parse(toolPath string, cmd string) ParseResult {
	// We need to check if the rule file exists and is valid via the yamlv3 library.
	toolFileBytes, err := os.ReadFile(toolPath)
	if err != nil {
		return ParseResult{
			Pass:   false,
			Reason: err,
			Rule:   "Tool path: " + toolPath,
		}
	}

	toolSpec := &tool.ToolSpec{}
	if err := yaml.Unmarshal(toolFileBytes, toolSpec); err != nil {
		return ParseResult{
			Pass:   false,
			Reason: err,
			Rule:   "Tool path: " + toolPath,
		}
	}

	ruleName := toolSpec.Name

	tokens, err := lex.Tokenize(cmd)
	if err != nil {
		return ParseResult{
			Pass:   false,
			Reason: err,
			Rule:   ruleName,
		}
	}

	fmt.Println(tokens)

	// TODO: Implement parsing logic
	return ParseResult{
		Pass:   true,
		Reason: nil,
		Rule:   ruleName,
	}
}
