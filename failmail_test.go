package main

import (
	"bufio"
	"fmt"
	p "github.com/hut8labs/failmail/parse"
	"os"
	"testing"
)

type ActionKind int

const UPSTREAM_CONN = "U"

const (
	DOWNSTREAM_SENDS ActionKind = iota
	DOWNSTREAM_EXPECTS
	UPSTREAM_SENDS
	UPSTREAM_EXPECTS
)

type TestAction struct {
	Conn string
	Kind ActionKind
	Body string
}

func buildAction(node *p.Node) *TestAction {
	conn, ok := node.Get("conn")
	if !ok {
		return nil
	}

	body, _ := node.Get("body")
	if !ok {
		return nil
	}

	var kind ActionKind
	if _, isSend := node.Get("send"); isSend {
		if conn.Text == UPSTREAM_CONN {
			kind = UPSTREAM_SENDS
		} else {
			kind = DOWNSTREAM_SENDS
		}
	} else if _, isRecv := node.Get("recv"); isRecv {
		if conn.Text == UPSTREAM_CONN {
			kind = UPSTREAM_EXPECTS
		} else {
			kind = DOWNSTREAM_EXPECTS
		}
	} else {
		panic("action was neither send nor receive")
	}
	return &TestAction{conn.Text, kind, body.Text}
}

func buildSpecParser() p.Parser {
	conn := p.Label("conn", p.Regexp(`\w+`))
	dir := p.Any(p.Label("send", p.Literal(">")), p.Label("recv", p.Literal("<")))
	body := p.Label("body", p.Regexp(`[^\n]+`))
	space := p.Regexp(`\s+`)
	return p.Series(conn, dir, space, body)
}

func parseTestActions(filename string) ([]*TestAction, error) {
	parser := buildSpecParser()

	results := make([]*TestAction, 0)

	file, err := os.Open(filename)
	if err != nil {
		return results, err
	}

	lineno := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		spec := scanner.Text()
		lineno += 1
		rest, node := parser.Parse(spec)
		if node == nil {
			return results, fmt.Errorf("failed to parse line %d: %#v", lineno, spec)
		} else if len(rest) > 0 {
			return results, fmt.Errorf("failed to consume all of line %d: %#v", lineno, rest)
		}

		action := buildAction(node)
		if action == nil {
			return results, fmt.Errorf("failed to read action in line %d: %#v", lineno, spec)
		}

		results = append(results, action)
	}
	if err = scanner.Err(); err != nil {
		return results, err
	}

	return results, nil
}

func playActions(actions []*TestAction) {
	// TODO
}

func TestSpecs(t *testing.T) {
	results, err := parseTestActions("testfiles/sample")
	if err != nil {
		t.Errorf("unexpected error parsing test spec: %s", err)
	}
	if numResults := len(results); numResults != 2 {
		t.Errorf("expected 2 test actions in sample.test, found %d", numResults)
	}
}
