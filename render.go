package main

import (
	"bytes"
	"fmt"
	"text/template"
	"time"
)

var SUMMARY_TEMPLATE_FUNCS template.FuncMap = map[string]interface{}{
	"time": func(t time.Time) string {
		return t.Format(time.RFC1123Z)
	},
}

type SummaryRenderer interface {
	Render(*SummaryMessage) OutgoingMessage
}

type NoRenderer struct{}

func (r *NoRenderer) Render(s *SummaryMessage) OutgoingMessage {
	return s
}

type TemplateRenderer struct {
	Template *template.Template
}

func normalizeNewlines(s string) []byte {
	buf := new(bytes.Buffer)
	for i, c := range s {
		if c == '\n' && i > 0 && s[i-1] != '\r' {
			buf.WriteString("\r\n")
		} else {
			buf.WriteRune(c)
		}
	}
	return buf.Bytes()
}

func (r *TemplateRenderer) Render(s *SummaryMessage) OutgoingMessage {
	buf := new(bytes.Buffer)
	err := r.Template.Execute(buf, s)
	if err != nil {
		fmt.Fprintf(buf, "\nError rendering message: %s\n", err)
	}
	return &message{s.From, s.To, normalizeNewlines(buf.String())}
}
