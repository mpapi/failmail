package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/hut8labs/failmail/parse"
	"net/mail"
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

type Session struct {
	Received *ReceivedMessage
}

func (s *Session) Start() Response {
	s.Received = &ReceivedMessage{}
	return Response{220, "localhost Hi there"}
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

// Reads the payload from a DATA command -- up to and including the "." on a
// newline by itself.
func (s *Session) ReadData(reader func() (string, error)) (Response, *ReceivedMessage) {
	data := new(bytes.Buffer)
	for {
		line, err := reader()
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

func (s *Session) Advance(node *parse.Node) Response {
	if node == nil {
		return Response{500, "Parse error"}
	}

	command, ok := node.Get("command")
	if !ok {
		return Response{500, "Parse error"}
	}

	switch strings.ToLower(command.Text) {
	case "quit":
		return Response{221, "localhost See ya"}
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
