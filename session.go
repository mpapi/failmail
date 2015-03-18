package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/hut8labs/failmail/parse"
	"log"
	"net/mail"
	"regexp"
	"strings"
)

type SessionSecurity int

const (
	UNENCRYPTED SessionSecurity = iota
	TLS_PRE_STARTTLS
	TLS_POST_STARTTLS
	SSL
)

func (s SessionSecurity) IsEncrypted() bool {
	return s == TLS_POST_STARTTLS || s == SSL
}

func (s SessionSecurity) AllowStarttls() bool {
	return s == TLS_PRE_STARTTLS
}

type AuthState int

const (
	NOT_PERMITTED AuthState = iota
	REQUIRED
	AUTHENTICATED
)

var pattern = regexp.MustCompile(`\d+`)

type Response struct {
	Code int
	Text string
}

func (r Response) IsClose() bool {
	return r.Code == 221
}

func (r Response) NeedsAuthResponse() bool {
	return r.Code == 334
}

func (r Response) NeedsData() bool {
	return r.Code == 354
}

func (r Response) StartsTLS() bool {
	return r.Text == "Ready to switch to TLS"
}

func (r Response) WriteTo(writer stringWriter) error {
	text := strings.TrimSpace(r.Text)
	lines := strings.Split(text, "\r\n")
	if len(lines) > 1 {
		for index, line := range lines {
			if index < len(lines)-1 {
				if _, err := writer.WriteString(fmt.Sprintf("%d-%s\r\n", r.Code, line)); err != nil {
					return err
				}
			} else {
				if _, err := writer.WriteString(fmt.Sprintf("%d %s\r\n", r.Code, line)); err != nil {
					return err
				}
			}
		}
	} else if _, err := writer.WriteString(fmt.Sprintf("%d %s\r\n", r.Code, r.Text)); err != nil {
		return err
	}
	return writer.Flush()
}

type stringReader interface {
	ReadString(delim byte) (string, error)
}

type debugReader struct {
	Reader stringReader
	Prefix string
}

func (r *debugReader) ReadString(delim byte) (string, error) {
	result, err := r.Reader.ReadString(delim)
	log.Printf("%s<<< %#v %v", r.Prefix, result, err)
	return result, err
}

type stringWriter interface {
	WriteString(string) (int, error)
	Flush() error
}

type debugWriter struct {
	Writer stringWriter
	Prefix string
}

func (w *debugWriter) WriteString(str string) (int, error) {
	log.Printf("%s>>> %#v", w.Prefix, str)
	return w.Writer.WriteString(str)
}

func (w *debugWriter) Flush() error {
	log.Printf("%s>>> (FLUSH)", w.Prefix)
	return w.Writer.Flush()
}

type Auth interface {
	ValidCredentials(string) (bool, error)
	IsPermitted(SessionSecurity) bool
}

type SingleUserPlainAuth struct {
	Username             string
	Password             string
	allowUnencryptedAuth bool
}

func (a *SingleUserPlainAuth) IsPermitted(security SessionSecurity) bool {
	return security.IsEncrypted() || a.allowUnencryptedAuth
}

func (a *SingleUserPlainAuth) ValidCredentials(token string) (bool, error) {
	parts := strings.Split(token, "\x00")
	if len(parts) != 3 {
		return false, fmt.Errorf("invalid token")
	}

	valid := parts[1] == a.Username && parts[2] == a.Password
	return valid, nil
}

type Session struct {
	Received  *ReceivedMessage
	hostname  string
	parser    Parser
	auth      Auth
	authState AuthState
	security  SessionSecurity
}

// Sets up a session and returns the `Response` that should be sent to a
// client immediately after it connects.
func (s *Session) Start(auth Auth, security SessionSecurity) Response {
	s.initHostname()
	s.parser = SMTPParser()
	s.Received = &ReceivedMessage{message: &message{}}
	s.auth = auth
	if s.auth == nil {
		s.authState = NOT_PERMITTED
	} else {
		s.authState = REQUIRED
	}
	s.security = security

	return Response{220, fmt.Sprintf("%s Hi there", s.hostname)}
}

func (s *Session) initHostname() {
	hostname, err := hostGetter()
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

		received.Data = []byte(data)
		received.Parsed = msg
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

func (s *Session) ReadAuthResponse(reader stringReader) Response {
	line, err := reader.ReadString('\n')
	if err != nil {
		return Response{500, "Parse error"}
	}
	return s.checkCredentials(line)
}

func (s *Session) authRequired(command *parse.Node) bool {
	switch strings.ToLower(command.Text) {
	case "quit", "helo", "ehlo", "rset", "noop", "auth", "starttls":
		return false
	}
	return s.authState == REQUIRED
}

func (s *Session) authenticate(method string, payload string) Response {
	switch {
	case method != "PLAIN":
		return Response{504, "Unrecognized authentication type"}
	case payload == "":
		return Response{334, ""}
	default:
		return s.checkCredentials(payload)
	}
}

func (s *Session) checkCredentials(payload string) Response {
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return Response{501, "Error decoding credentials"}
	}

	valid, err := s.auth.ValidCredentials(string(data))
	if err != nil {
		return Response{501, "Error validating credentials"}
	}

	if !valid {
		return Response{535, "Authentication failed"}
	} else {
		s.authState = AUTHENTICATED
		return Response{235, "Authentication successful"}
	}
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
		text := "Hello\r\nAUTH PLAIN"
		if s.security.AllowStarttls() {
			text += "\r\nSTARTTLS"
		}
		return Response{250, text}
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
	case "auth":
		if s.authState == REQUIRED && !s.auth.IsPermitted(s.security) {
			return Response{502, "An encrypted connection is required for authentication"}
		} else if s.authState == AUTHENTICATED {
			return Response{503, "Already authenticated"}
		} else if s.authState == NOT_PERMITTED {
			return Response{502, "Authentication is not supported"}
		}
		authType := node.Children["type"].Text
		if payload, ok := node.Get("payload"); ok {
			return s.authenticate(authType, payload.Text)
		} else {
			return s.authenticate(authType, "")
		}
	case "starttls":
		if s.security == TLS_POST_STARTTLS {
			return Response{500, "Already using TLS"}
		} else if !s.security.AllowStarttls() {
			return Response{500, "STARTTLS not supported"}
		}
		return Response{220, "Ready to switch to TLS"}
	default:
		return Response{502, "Not implemented"}
	}
}
