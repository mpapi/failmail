package main

import (
	"reflect"
	"regexp"
	"testing"
)

func TestRewriteEverything(t *testing.T) {
	rewriter := &AddressRewriter{regexp.MustCompile(`.*`), "user@example.com"}

	if addr := rewriter.Rewrite("test@example.com"); addr != "user@example.com" {
		t.Errorf("expected no rewrite for test@example.com, got %s", addr)
	}
}

func TestRewriteMatching(t *testing.T) {
	rewriter := &AddressRewriter{regexp.MustCompile(`failmail\+([^@]*)@example.com`), "$1@example.com"}

	if addr := rewriter.Rewrite("test@example.com"); addr != "test@example.com" {
		t.Errorf("expected no rewrite for test@example.com, got %s", addr)
	}

	if addr := rewriter.Rewrite("failmail+user@example.com"); addr != "user@example.com" {
		t.Errorf("expected rewrite to user@example.com, got %s", addr)
	}
}

func TestRewriteAll(t *testing.T) {
	rewriter := &AddressRewriter{regexp.MustCompile(`failmail\+([^@]*)@example.com`), "$1@example.com"}

	results := rewriter.RewriteAll([]string{"test@example.com", "failmail+test@example.com", "root@example.com"})
	if !reflect.DeepEqual(results, []string{"root@example.com", "test@example.com"}) {
		t.Errorf("expected 2 unique rewritten addresses, got %v", results)
	}
}
