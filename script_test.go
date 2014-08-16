package main

import (
	"testing"
)

func TestSpecs(t *testing.T) {
	results, err := parseTestActions("testfiles/sample")
	if err != nil {
		t.Errorf("unexpected error parsing test spec: %s", err)
	}
	if numResults := len(results); numResults != 2 {
		t.Errorf("expected 2 test actions in sample.test, found %d", numResults)
	}
}
