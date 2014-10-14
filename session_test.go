package main

import (
	"bufio"
	"bytes"
	"fmt"
	p "github.com/hut8labs/failmail/parse"
	"testing"
)

type mockStringReader struct {
	data string
	err  error
}

func (m mockStringReader) ReadString(delim byte) (string, error) {
	return m.data, m.err
}

func TestResponseIsClose(t *testing.T) {
	r := Response{221, "Whatever"}
	if !r.IsClose() {
		t.Errorf("expected 221 IsClose()")
	}

	r = Response{200, "Whatever"}
	if r.IsClose() {
		t.Errorf("expected non-221 !IsClose()")
	}
}

func TestResponseNeedsData(t *testing.T) {
	r := Response{354, "Whatever"}
	if !r.NeedsData() {
		t.Errorf("expected 354 NeedsData()")
	}

	r = Response{200, "Whatever"}
	if r.NeedsData() {
		t.Errorf("expected non-354 !NeedsData()")
	}
}

func TestResponseWriteTo(t *testing.T) {
	output := new(bytes.Buffer)
	buf := bufio.NewWriter(output)

	Response{220, "Hello"}.WriteTo(buf)

	contents := string(output.Bytes())
	if contents != "220 Hello\r\n" {
		t.Errorf("unexpected response: %s", contents)
	}
}

func TestResponseWriteToMultiLine(t *testing.T) {
	output := new(bytes.Buffer)
	buf := bufio.NewWriter(output)

	Response{250, "host1.example.com Hello host2.example.com\r\nAUTH PLAIN"}.WriteTo(buf)

	contents := string(output.Bytes())
	if contents != "250-host1.example.com Hello host2.example.com\r\n250 AUTH PLAIN\r\n" {
		t.Errorf("unexpected response: %s", contents)
	}
}

func TestSessionStart(t *testing.T) {
	s := new(Session)
	resp := s.Start(nil, false)

	if s.Received == nil {
		t.Errorf("start should set up a message")
	}

	if resp.Code != 220 {
		t.Errorf("start should return a 220 response")
	}
}

func TestSessionReadCommand(t *testing.T) {
	s := new(Session)
	s.Start(nil, false)

	buf := bytes.NewBufferString("HELO test.example.com\r\n")
	resp, err := s.ReadCommand(buf)

	if err != nil {
		t.Errorf("ReadCommand of a valid string shouldn't return an error")
	}

	if resp.Code != 250 {
		t.Errorf("HELO should get a 250 response")
	}

	resp, err = s.ReadCommand(mockStringReader{"", fmt.Errorf("Error")})
	if err == nil {
		t.Errorf("ReadCommand should return an error when the reader does")
	}

	if resp.Code != 500 {
		t.Errorf("ReadCommand should return a 500 response when the reader errors")
	}
}

func TestSessionAdvance(t *testing.T) {
	s := new(Session)
	s.Start(nil, false)

	if resp := s.Advance(nil); resp.Code != 500 {
		t.Errorf("nil node is not a parse error")
	}

	if resp := s.Advance(&p.Node{Text: "", Children: make(map[string]*p.Node), Next: nil}); resp.Code != 500 {
		t.Errorf("empty node is not a parse error")
	}

	parser := SMTPParser()

	if resp := s.Advance(parser("HELO test.example.com\r\n")); resp.Code != 250 {
		t.Errorf("HELO should get a 250 response")
	}

	if resp := s.Advance(parser("EHLO test.example.com\r\n")); resp.Code != 250 {
		t.Errorf("EHLO should get a 250 response")
	}

	if resp := s.Advance(parser("NOOP\r\n")); resp.Code != 250 {
		t.Errorf("NOOP should get a 250 response")
	}

	if s.Received.From != "" {
		t.Errorf("from shouldn't be set before MAIL command")
	}

	if resp := s.Advance(parser("RCPT TO:<test@example.com>\r\n")); resp.Code != 503 {
		t.Errorf("RCPT before FROM should get a 503 response")
	}

	buf := bytes.NewBufferString(".\r\n")
	if resp, msg := s.ReadData(buf); resp.Code != 503 || msg != nil {
		t.Errorf("data read before FROM should get a 503 response")
	}

	if resp := s.Advance(parser("MAIL FROM:<test@example.com>\r\n")); resp.Code != 250 {
		t.Errorf("FROM should get a 250 response")
	}

	if s.Received.From != "test@example.com" {
		t.Errorf("unexpected from after MAIL command")
	}

	if resp := s.Advance(parser("MAIL FROM:<test@example.com>\r\n")); resp.Code != 503 {
		t.Errorf("repeated MAIL should get a 503 response")
	}

	if len(s.Received.To) > 0 {
		t.Errorf("to shouldn't be set before RCPT command")
	}

	if resp := s.Advance(parser("RCPT TO:<test1@example.com>\r\n")); resp.Code != 250 {
		t.Errorf("RCPT should get a 250 response")
	}

	if !(len(s.Received.To) == 1 && s.Received.To[0] == "test1@example.com") {
		t.Errorf("unexpected to after first RCPT command")
	}

	if resp := s.Advance(parser("RCPT TO:<test2@example.com>\r\n")); resp.Code != 250 {
		t.Errorf("RCPT should get a 250 response")
	}

	if !(len(s.Received.To) == 2 && s.Received.To[1] == "test2@example.com") {
		t.Errorf("unexpected to after second RCPT command")
	}

	if resp := s.Advance(parser("RCPT TO:<test2@example.com>\r\n")); resp.Code != 250 {
		t.Errorf("RCPT should get a 250 response")
	}

	if resp := s.Advance(parser("DATA\r\n")); resp.Code != 354 {
		t.Errorf("DATA should get a 354 response")
	}

	buf = bytes.NewBufferString("\x00\xff\r\n.\r\n")
	if resp, msg := s.ReadData(buf); resp.Code != 451 || msg != nil {
		t.Fatalf("bad data read should get a 451 response: %d", resp.Code)
	}

	if resp, msg := s.ReadData(mockStringReader{"", fmt.Errorf("error")}); resp.Code != 451 || msg != nil {
		t.Errorf("expected a 451 from an error reading DATA")
	}

	// TODO data followed by anything else should fail
	buf = bytes.NewBufferString("Subject: test\r\n\r\ntest\r\n.\r\n")
	resp, msg := s.ReadData(buf)
	if resp.Code != 250 || msg == nil {
		t.Errorf("DATA payload should get a 250 response")
	}

	subject := msg.Parsed.Header.Get("subject")
	if subject != "test" {
		t.Errorf("failed to parse subject from data payload: %s", subject)
	}

	if resp := s.Advance(parser("RSET\r\n")); resp.Code != 502 {
		t.Errorf("RSET should get a 502 response")
	}

	if resp := s.Advance(parser("VRFY test\r\n")); resp.Code != 252 {
		t.Errorf("VRFY should get a 252 response, got: %d", resp.Code)
	}

	if resp := s.Advance(parser("QUIT\r\n")); resp.Code != 221 {
		t.Errorf("QUIT should get a 221 response")
	}
}

func TestSingleUserPlainAuth(t *testing.T) {
	auth := &SingleUserPlainAuth{Username: "testuser", Password: "testpass"}
	valid, err := auth.ValidCredentials("testuser\x00testuser\x00testpass")

	if !valid {
		t.Errorf("expected valid credentials")
	}

	if err != nil {
		t.Errorf("expected no errors validating credentials")
	}
}

func TestSingleUserPlainAuthBadCredentials(t *testing.T) {
	auth := &SingleUserPlainAuth{Username: "testuser", Password: "testpass"}
	valid, err := auth.ValidCredentials("testuser\x00testuser\x00badpass")

	if valid {
		t.Errorf("expected invalid credentials")
	}

	if err != nil {
		t.Errorf("expected no errors validating credentials")
	}
}

func TestSingleUserPlainAuthError(t *testing.T) {
	auth := &SingleUserPlainAuth{Username: "testuser", Password: "testpass"}
	valid, err := auth.ValidCredentials("testuser\x00testpass")

	if valid {
		t.Errorf("expected invalid credentials")
	}

	if err == nil {
		t.Errorf("expected errors validating credentials")
	}
}

func TestAuthRequired(t *testing.T) {
	auth := &SingleUserPlainAuth{Username: "testuser", Password: "testpass"}

	parser := SMTPParser()

	s := new(Session)
	s.Start(auth, false)

	if resp := s.Advance(parser("HELO test.example.com\r\n")); resp.Code != 250 {
		t.Errorf("HELO should get a 250 response")
	}

	if resp := s.Advance(parser("VRFY test\r\n")); resp.Code != 530 {
		t.Errorf("VRFY with auth required should get a 530 response")
	}
}

func TestAuthBadMethod(t *testing.T) {
	auth := &SingleUserPlainAuth{Username: "testuser", Password: "testpass"}

	parser := SMTPParser()

	s := new(Session)
	s.Start(auth, false)

	if resp := s.Advance(parser("HELO test.example.com\r\n")); resp.Code != 250 {
		t.Errorf("HELO should get a 250 response")
	}

	if resp := s.Advance(parser("AUTH BADMETHOD\r\n")); resp.Code != 504 {
		t.Errorf("AUTH with a bad method should get a 504 response")
	}
}

func TestAuthBadCredentials(t *testing.T) {
	auth := &SingleUserPlainAuth{Username: "testuser", Password: "testpass"}

	parser := SMTPParser()

	s := new(Session)
	s.Start(auth, false)

	if resp := s.Advance(parser("HELO test.example.com\r\n")); resp.Code != 250 {
		t.Errorf("HELO should get a 250 response")
	}

	if resp := s.Advance(parser("AUTH PLAIN notbase64\r\n")); resp.Code != 501 {
		t.Errorf("AUTH with a non-base64 payload should get a 501 response")
	}
}
