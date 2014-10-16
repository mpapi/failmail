package main

import (
	"strings"
	"testing"
	"text/template"
)

func TestRenderBadTemplate(t *testing.T) {
	r := &TemplateRenderer{template.Must(template.New("test").Parse("{{.bad}}"))}
	msg := r.Render(makeSummaryMessage(t, "From: test@example.com\r\nTo: test@example.com\r\nSubject: test\r\n\r\ntest message\r\n"))
	body := strings.TrimSpace(string(msg.Contents()))
	if !strings.HasPrefix(body, "Error rendering message") {
		t.Errorf("expected the outgoing message body to report an error")
	}
}
