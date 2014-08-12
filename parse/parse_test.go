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

func TestOmit(t *testing.T) {
	parser := Omit(Literal("test"))
	rest, parsed := parser.Parse("test 123")
	if rest != " 123" {
		t.Errorf("parser left unexpected string: %s", rest)
	}
	if parsed == nil || parsed.Text != "" || len(parsed.Children) > 0 {
		t.Errorf("unexpected parse result: %#v", parsed)
	}
}

func TestLabel(t *testing.T) {
	parser := Label("value", Longest(Literal("test"), Literal("testing")))
	rest, parsed := parser.Parse("testing 123")
	if rest != " 123" {
		t.Errorf("parser left unexpected string: %s", rest)
	}
	if parsed == nil || parsed.Text != "testing" {
		t.Errorf("parsed unexpected fragment: %s", parsed)
	} else if labeled, ok := parsed.Get("value"); !ok || labeled.Text != "testing" {
		t.Errorf("unexpected parse result: %s", labeled)
	}
}

func TestSeries(t *testing.T) {
	parser := Series(Label("first", Longest(Literal("test"), Literal("testing"))),
		Label("second", Literal(" 123")))
	rest, parsed := parser.Parse("testing 123")
	if rest != "" {
		t.Errorf("parser left unexpected string: %s", rest)
	}
	if parsed == nil || parsed.Text != "testing 123" {
		t.Errorf("parsed unexpected fragment: %s", parsed)
	} else {
		if labeled, ok := parsed.Get("first"); !ok || labeled.Text != "testing" {
			t.Errorf("unexpected parse result: %s", labeled)
		}
		if labeled, ok := parsed.Get("second"); !ok || labeled.Text != " 123" {
			t.Errorf("unexpected parse result: %s", labeled)
		}
	}
}

func TestRegexp(t *testing.T) {
	parser := Regexp(`\w+ \d+`)
	rest, parsed := parser.Parse("testing 123")
	if rest != "" {
		t.Errorf("parser left unexpected string: %s", rest)
	}
	if parsed == nil || parsed.Text != "testing 123" {
		t.Errorf("parsed unexpected fragment: %s", parsed)
	}

	rest, parsed = parser.Parse("123 testing")
	if rest != "123 testing" {
		t.Errorf("parser left unexpected string: %s", rest)
	}
	if parsed != nil {
		t.Errorf("parsed unexpected fragment: %s", parsed)
	}
}

func TestRepeat(t *testing.T) {
	parser := Repeat(3, Series(Regexp(`\w+`), Literal("\n")))
	rest, parsed := parser.Parse("first\nsecond\nthird\n")
	if rest != "" {
		t.Errorf("parser left unexpected string: %s", rest)
	}
	if parsed == nil || parsed.Text != "first\nsecond\nthird\n" {
		t.Errorf("parsed unexpected fragment: %s", parsed)
	}
}

func TestSurrounding(t *testing.T) {
	parser := Surrounding(`"`, `"`, Regexp(`\w+`))
	rest, parsed := parser.Parse(`"testing"`)
	if rest != "" {
		t.Errorf("parser left unexpected string: %s", rest)
	}
	if parsed == nil || parsed.Text != "testing" {
		t.Errorf("parsed unexpected fragment: %s", parsed)
	}
}
