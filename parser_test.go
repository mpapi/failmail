package main

import (
	p "github.com/hut8labs/failmail/parse"
	"testing"
)

type ParserTestCase struct {
	Verify func(*p.Node) bool
	Input  string
}

func failed(node *p.Node) bool {
	return node == nil
}

func ok(node *p.Node) bool {
	return node != nil
}

var parserTests = []ParserTestCase{
	ParserTestCase{ok, "HELO example.com\r\n"},
	ParserTestCase{failed, "HELO\r\n"},
	ParserTestCase{ok, "VRFY user\r\n"},
	ParserTestCase{ok, "AUTH PLAIN dGVzdA==\r\n"},
	ParserTestCase{failed, "AUTH badtype dGVzdA==\r\n"},
	ParserTestCase{failed, "AUTH PLAIN notb64*=\r\n"},
}

func TestSMTPParser(t *testing.T) {
	parser := SMTPParser()

	for _, test := range parserTests {
		result := parser(test.Input)
		if !test.Verify(result) {
			t.Errorf("unexpected parse result for %s", test.Input)
		}
	}
}
