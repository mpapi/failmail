package parse

import (
	"testing"
)

func TestLiteral(t *testing.T) {
	rest, parsed := Literal("test").Parse("test 123")
	if rest != " 123" {
		t.Errorf("parser left unexpected string: %s", rest)
	}
	if parsed == nil || parsed.Text != "test" {
		t.Errorf("parsed unexpected fragment: %s", parsed)
	}
}

func TestInvalid(t *testing.T) {
	rest, parsed := Literal("test").Parse("123")
	if rest != "123" {
		t.Errorf("parser left unexpected string: %s", rest)
	}
	if parsed != nil {
		t.Errorf("expected nil parse result from failed parse, got %s", parsed)
	}
}

func TestLongest(t *testing.T) {
	parser := Longest(Literal("test"), Literal("testing"))
	rest, parsed := parser.Parse("testing 123")
	if rest != " 123" {
		t.Errorf("parser left unexpected string: %s", rest)
	}
	if parsed == nil || parsed.Text != "testing" {
		t.Errorf("parsed unexpected fragment: %s", parsed)
	}
}