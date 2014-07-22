package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/hut8labs/failmail/parse"
	"net/mail"
	"os"
	"regexp"
	"strings"
)

var pattern = regexp.MustCompile(`\d+`)

type Response struct {
	Code int
	Text string
}

func (r Response) IsClose() bool {
	return r.Code == 221
}

func (r Response) NeedsData() bool {
	return r.Code == 354
}

// TODO return error
func (r Response) WriteTo(writer *bufio.Writer) {
	writer.WriteString(fmt.Sprintf("%d %s\r\n", r.Code, r.Text))
	writer.Flush()
}

type stringReader interface {
	ReadString(delim byte) (string, error)
}

type Session struct {
	Received      *ReceivedMessage
	Authenticated bool
	hostname      string
	parser        Parser
}

// Sets up a session and returns the `Response` that should be sent to a
// client immediately after it connects.
func (s *Session) Start(authRequired bool) Response {
	s.initHostname()
	s.parser = SMTPParser()
	s.Received = &ReceivedMessage{}
	s.Authenticated = !authRequired

	return Response{220, fmt.Sprintf("%s Hi there", s.hostname)}
}

func (s *Session) initHostname() {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	s.hostname = hostname
}

func (s *Session) setFrom(from string) Response {
	if len(s.Received.From) > 0 || len(s.Received.To) > 0 || len(s.Received.Data) > 0 {
		return Response{503, "Command out of sequence"}
	}
	s.Received.From = from
	return Response{250, "OK"}
}

func (s *Session) addTo(to string) Response {
	if len(s.Received.From) == 0 || len(s.Received.Data) > 0 {
		return Response{503, "Command out of sequence"}
	}
	s.Received.To = append(s.Received.To, to)
	return Response{250, "OK"}
}

func (s *Session) setData(data string) (Response, *ReceivedMessage) {
	if len(s.Received.From) == 0 || len(s.Received.To) == 0 || len(s.Received.Data) > 0 {
		return Response{503, "Command out of sequence"}, nil
	}
	buf := bytes.NewBufferString(data)
	if msg, err := mail.ReadMessage(buf); err != nil {
		return Response{451, "Failed to parse data"}, nil
	} else {
		received := s.Received
		s.Received = &ReceivedMessage{}

		received.Data = data
		received.Message = msg
		return Response{250, "Got the data"}, received
	}
}

// Reads and parses a single command and advances the session accordingly.  In
// case of error, returns either a non-nil error (if the command couldn't be
// read from the `reader`) or a `Response` with the appropriate SMTP error code
// (for other error conditions).
func (s *Session) ReadCommand(reader stringReader) (Response, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return Response{500, "Parse error"}, err
	}
	return s.Advance(s.parser(line)), nil
}

// Reads the payload from a DATA command -- up to and including the "." on a
// newline by itself.
func (s *Session) ReadData(reader stringReader) (Response, *ReceivedMessage) {
	data := new(bytes.Buffer)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return Response{451, "Failed to read data"}, nil
		}

		if line == ".\r\n" {
			break
		} else {
			data.WriteString(line)
		}
	}
	return s.setData(data.String())
}

func (s *Session) authRequired(command *parse.Node) bool {
	switch strings.ToLower(command.Text) {
	case "quit", "helo", "ehlo", "rset", "noop":
		return false
	}
	return !s.Authenticated
}

// Advances the state of the session according to the parsed SMTP command, and
// returns an appropriate `Response`. For example, the MAIL command modifies
// the session to store the sender's address and to expect future commands to
// specify the recipients and body of the message.
func (s *Session) Advance(node *parse.Node) Response {
	if node == nil {
		return Response{500, "Parse error"}
	}

	command, ok := node.Get("command")
	if !ok {
		return Response{500, "Parse error"}
	}

	if s.authRequired(command) {
		return Response{530, "Authentication required"}
	}

	switch strings.ToLower(command.Text) {
	case "quit":
		return Response{221, fmt.Sprintf("%s See ya", s.hostname)}
	case "helo":
		return Response{250, "Hello"}
	case "ehlo":
		return Response{250, "Hello"}
	case "noop":
		return Response{250, "Noop"}
	case "rcpt":
		return s.addTo(node.Children["path"].Text)
	case "mail":
		return s.setFrom(node.Children["path"].Text)
	case "vrfy":
		return Response{252, "Maybe"}
	case "data":
		return Response{354, "Go"}
	default:
		return Response{502, "Not implemented"}
	}
}
