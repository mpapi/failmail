package configure

import (
	"bytes"
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

func TestNormalizeFlag(t *testing.T) {
	if result := normalizeFlag("Test"); result != "test" {
		t.Errorf("expected 'test-flag', got %#v", result)
	}

	if result := normalizeFlag("TestFlag"); result != "test-flag" {
		t.Errorf("expected 'test-flag', got %#v", result)
	}

	if result := normalizeFlag("TestHTTP"); result != "test-http" {
		t.Errorf("expected 'test-flag', got %#v", result)
	}

	if result := normalizeFlag("test_HTTP"); result != "test-http" {
		t.Errorf("expected 'test-flag', got %#v", result)
	}
}

type ReadConfigTest struct {
	First  int
	Second string
	Third  bool
}

func TestReadConfig(t *testing.T) {
	buffer := bytes.NewBufferString("# A comment\n\nfirst = 1\nsecond = 2\nthird = true\n")
	config := &ReadConfigTest{}
	err := ReadConfig(buffer, config)
	if err != nil {
		t.Fatalf("unexpected error reading config")
	}

	if config.First != 1 {
		t.Errorf("Expected First = 1, got %d", config.First)
	}
	if config.Second != "2" {
		t.Errorf("Expected Second = \"2\", got %s", config.Second)
	}
	if !config.Third {
		t.Errorf("Expected Third = true, got %v", config.Third)
	}
}
