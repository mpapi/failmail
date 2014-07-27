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
