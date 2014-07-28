package parse

import (
	"fmt"
	"regexp"
	"strings"
)

type Node struct {
	Text     string
	Children map[string]*Node
}

func (n *Node) Get(token string) (*Node, bool) {
	if n.Children == nil {
		return nil, false
	}
	val, ok := n.Children[token]
	return val, ok
}

func (n *Node) String() string {
	return fmt.Sprintf("text=%#v children=%v\n", n.Text, n.Children)
}

// Parsers have a `Parse` method that takes a string and returns the remainder
// of the string after parsing, and a `Node` for the parse result.
type Parser interface {
	Parse(str string) (string, *Node)
}

type parseAny struct {
	Parsers []Parser
}

func Any(parsers ...Parser) *parseAny {
	return &parseAny{parsers}
}

func (p *parseAny) Add(parsers ...Parser) *parseAny {
	p.Parsers = append(p.Parsers, parsers...)
	return p
}

func (p *parseAny) Parse(str string) (string, *Node) {
	for _, parser := range p.Parsers {
		if rest, node := parser.Parse(str); node != nil {
			return rest, node
		}
	}
	return str, nil
}

type parseLongest struct {
	Parsers []Parser
}

func Longest(parsers ...Parser) *parseLongest {
	return &parseLongest{parsers}
}

func (p *parseLongest) Add(parsers ...Parser) *parseLongest {
	p.Parsers = append(p.Parsers, parsers...)
	return p
}

func (p *parseLongest) Parse(str string) (string, *Node) {
	longestRest := ""
	longestNode := (*Node)(nil)
	for _, parser := range p.Parsers {
		rest, node := parser.Parse(str)
		if longestNode == nil || (node != nil && len(rest) < len(longestRest)) {
			longestRest = rest
			longestNode = node
		}
	}
	return longestRest, longestNode
}

// Construct that parses but throws away the result.
type parseOmit struct {
	Parser Parser
}

func (p *parseOmit) Parse(str string) (string, *Node) {
	node := &Node{"", make(map[string]*Node)}
	rest, child := p.Parser.Parse(str)
	if child == nil {
		return str, nil
	}
	return rest, node
}

func Omit(parser Parser) *parseOmit {
	return &parseOmit{parser}
}

type parseLabel struct {
	Label  string
	Parser Parser
}

func (p *parseLabel) Parse(str string) (string, *Node) {
	rest, child := p.Parser.Parse(str)
	if child == nil {
		return str, nil
	}
	return rest, &Node{child.Text, map[string]*Node{p.Label: child}}
}

func Label(label string, parser Parser) *parseLabel {
	return &parseLabel{label, parser}
}

type parseSeries struct {
	Parsers []Parser
}

func Series(parsers ...Parser) *parseSeries {
	return &parseSeries{parsers}
}

func (p *parseSeries) Add(parsers ...Parser) *parseSeries {
	p.Parsers = append(p.Parsers, parsers...)
	return p
}

func (p *parseSeries) Parse(str string) (string, *Node) {
	node := &Node{"", make(map[string]*Node)}
	var rest string = str
	var child *Node
	for _, parser := range p.Parsers {
		rest, child = parser.Parse(rest)
		if child == nil {
			return str, nil
		}
		node.Text += child.Text
		for key, value := range child.Children {
			node.Children[key] = value
		}
	}
	return rest, node
}

type parseLiteral struct {
	Literal       string
	CaseSensitive bool
}

func Literal(lit string) *parseLiteral {
	return &parseLiteral{lit, true}
}

func ILiteral(lit string) *parseLiteral {
	return &parseLiteral{strings.ToLower(lit), false}
}

func (p *parseLiteral) Parse(str string) (string, *Node) {
	check := str
	if !p.CaseSensitive {
		check = strings.ToLower(str)
	}
	if !strings.HasPrefix(check, p.Literal) {
		return str, nil
	}
	return str[len(p.Literal):], &Node{str[:len(p.Literal)], nil}
}

type parseRegexp struct {
	Regexp *regexp.Regexp
}

func Regexp(re string) *parseRegexp {
	pat := regexp.MustCompile("^" + re)
	pat.Longest()
	return &parseRegexp{pat}
}

func (p *parseRegexp) Parse(str string) (string, *Node) {
	match := p.Regexp.FindString(str)
	if len(match) == 0 {
		return str, nil
	}
	return str[len(match):], &Node{match, nil}
}

func Repeat(times int, p Parser) Parser {
	parser := Series()
	for i := 0; i < times; i++ {
		parser.Add(p)
	}
	return parser
}

func Surrounding(start string, end string, parser Parser) Parser {
	return Series(Omit(Literal(start)), parser, Omit(Literal(end)))
}

func Separating(str string, parsers ...Parser) Parser {
	parser := Series()
	for _, p := range parsers {
		if len(parser.Parsers) > 0 {
			parser.Add(Literal(str))
		}
		parser.Add(p)
	}
	return parser
}
