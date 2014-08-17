package configure

import (
	"testing"
)

func TestConfigParser(t *testing.T) {
	parser := ConfigParser()

	rest, parsed := parser.Parse("# A comment\n\ntest1 = true\ntest2=false\n")
	if rest != "" {
		t.Errorf("parser left unexpected string: %s", rest)
	}

	if parsed == nil || parsed.Text != "test1 = true\ntest2=false\n" {
		t.Errorf("parsed unexpected fragment: %v", parsed)
	}

	comment := parsed.Next
	blank := comment.Next
	first := blank.Next
	second := first.Next

	if key, ok := first.Get("key"); !ok || key.Text != "test1" {
		t.Errorf("expected first result key = test1, got %s", key)
	}
	if value, ok := first.Get("value"); !ok || value.Text != "true" {
		t.Errorf("expected first result value = true, got %s", value)
	}

	if key, ok := second.Get("key"); !ok || key.Text != "test2" {
		t.Errorf("expected second result key = test2, got %s", key)
	}
	if value, ok := second.Get("value"); !ok || value.Text != "false" {
		t.Errorf("expected second result value = false, got %s", value)
	}
}
