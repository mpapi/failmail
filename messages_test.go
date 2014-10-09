package main

import (
	"fmt"
	"net/mail"
	"reflect"
	"testing"
	"text/template"
	"time"
)

type BadReader struct{}

func (r BadReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("bad reader")
}

func TestReceivedMessageReadBody(t *testing.T) {
	msg := makeReceivedMessage(t, "Subject: test\r\n\r\ntest body\r\n")
	if body := msg.ReadBody(); body != "test body\r\n" {
		t.Errorf("unexpected message body: %s", body)
	}
	if body := msg.ReadBody(); body != "" {
		t.Errorf("unexpected message body on 2nd call: %s", body)
	}
}

func TestReceivedMessageReadBodyMissingMessage(t *testing.T) {
	msg := &ReceivedMessage{
		message: &message{From: "test@example.com", To: []string{"test@example.com"}},
		Parsed:  &mail.Message{Body: BadReader{}},
	}
	if body := msg.ReadBody(); body != "[unreadable message body]" {
		t.Errorf("unexpected message body for nil message: %s", body)
	}
}

func TestReceivedMessageReadBodyUnreadableMessage(t *testing.T) {
	msg := &ReceivedMessage{
		message: &message{From: "test@example.com", To: []string{"test@example.com"}},
	}
	if body := msg.ReadBody(); body != "[no message body]" {
		t.Errorf("unexpected message body for nil message: %s", body)
	}
}

func TestReceivedMessageDisplayDate(t *testing.T) {
	msg := makeReceivedMessage(t, "Subject: test\r\n\r\ntest body\r\n")
	if date := msg.DisplayDate("default"); date != "default" {
		t.Errorf("unexpected display date: %s", date)
	}

	msg = makeReceivedMessage(t, "Date: Wed, 16 Jul 2014 16:00:00 -0400\r\nSubject: test\r\n\r\ntest body\r\n")
	if date := msg.DisplayDate("default"); date != "Wed, 16 Jul 2014 16:00:00 -0400" {
		t.Errorf("unexpected display date: %s", date)
	}
}

func TestReceivedMessageOutgoing(t *testing.T) {
	msg := makeReceivedMessage(t, "From: test@example.com\r\nTo: test2@example.com\r\nSubject: test\r\n\r\ntest body\r\n")

	if from := msg.Sender(); from != "test@example.com" {
		t.Errorf("unexpected from: %s", from)
	}
	if to := msg.Recipients(); len(to) != 1 || to[0] != "test2@example.com" {
		t.Errorf("unexpected to: %s", to)
	}
	if body := msg.ReadBody(); body != "test body\r\n" {
		t.Errorf("unexpected body: %s", body)
	}
}

func TestCompact(t *testing.T) {
	msg1 := makeReceivedMessage(t, "From: test@example.com\r\nTo: test2@example.com\r\nDate: Tue, 01 Jul 2014 12:34:56 -0400\r\nSubject: test\r\n\r\ntest body 1\r\n")
	msg2 := makeReceivedMessage(t, "From: test@example.com\r\nTo: test2@example.com\r\nDate: Wed, 02 Jul 2014 12:34:56 -0400\r\nSubject: test\r\n\r\ntest body 2\r\n")
	uniques := Compact(GroupByExpr("batch", `{{.Header.Get "Subject"}}`), []*ReceivedMessage{msg1, msg2})
	if count := len(uniques); count != 1 {
		t.Errorf("expected one unique message from Compact(), got %d", count)
	}

	unique := uniques[0]
	if start := unique.Start.Format(time.RFC1123Z); start != "Tue, 01 Jul 2014 12:34:56 -0400" {
		t.Errorf("unexpected range start from Compact(): %s", start)
	}
	if end := unique.End.Format(time.RFC1123Z); end != "Wed, 02 Jul 2014 12:34:56 -0400" {
		t.Errorf("unexpected range end from Compact(): %s", end)
	}
	if unique.Subject != "test" {
		t.Errorf("unexpected subject from Compact(): %s", unique.Subject)
	}
	if unique.Body != "test body 2\r\n" {
		t.Errorf("unexpected body from Compact(): %s", unique.Body)
	}
	if unique.Count != 2 {
		t.Errorf("unexpected count from Compact(): %d", unique.Count)
	}
}

func TestSummarize(t *testing.T) {
	defer patchTime(time.Date(2014, time.March, 1, 0, 0, 0, 0, time.UTC))()
	msg1 := makeReceivedMessage(t, "From: test@example.com\r\nTo: test2@example.com\r\nDate: Tue, 01 Jul 2014 12:34:56 -0400\r\nSubject: test\r\n\r\ntest body 1\r\n")
	msg2 := makeReceivedMessage(t, "From: test@example.com\r\nTo: test3@example.com\r\nDate: Wed, 02 Jul 2014 12:34:56 -0400\r\nSubject: test 2\r\n\r\ntest body 2\r\n")

	summarized := Summarize(GroupByExpr("group", `{{.Header.Get "Subject"}}`), "failmail@example.com", "test2@example.com", []*ReceivedMessage{msg1, msg2})

	if summarized.From != "failmail@example.com" {
		t.Errorf("unexpected from address from Summarize(): %s", summarized.From)
	}
	if !reflect.DeepEqual(summarized.To, []string{"test2@example.com"}) {
		t.Errorf("unexpected to address from Summarize(): %#v", summarized.To)
	}
	if summarized.Subject != "[failmail] 2 instances of 2 messages" {
		t.Errorf("unexpected subject from Summarize(): %s", summarized.Subject)
	}
	if headers := summarized.Headers(); headers != "From: failmail@example.com\r\nTo: test2@example.com\r\nSubject: [failmail] 2 instances of 2 messages\r\nDate: 01 Mar 14 00:00 UTC\r\n\r\n" {
		t.Errorf("unexpected headers from Summarize(): %s", headers)
	}
}

func TestMessageBuffer(t *testing.T) {
	buf := NewMessageBuffer(5*time.Second, 9*time.Second, GroupByExpr("batch", `{{.Header.Get "Subject"}}`), GroupByExpr("group", `{{.Header.Get "Subject"}}`), NewMemoryStore(), "test@example.com")

	unpatch := patchTime(time.Unix(1393650000, 0))
	defer unpatch()
	buf.Add(makeReceivedMessage(t, "To: test@example.com\r\nSubject: test\r\n\r\ntest 1"))
	if count := buf.Stats().ActiveBatches; count != 1 {
		t.Errorf("unexpected buffer message count: %d", count)
	}
	unpatch()

	unpatch = patchTime(time.Unix(1393650004, 0))
	if summaries := buf.Flush(false); len(summaries) != 0 {
		t.Errorf("unexpected summaries from flush: %v", summaries)
	}
	unpatch()

	unpatch = patchTime(time.Unix(1393650005, 0))
	buf.Add(makeReceivedMessage(t, "To: test@example.com\r\nSubject: test\r\n\r\ntest 2"))
	if count := buf.Stats().ActiveBatches; count != 1 {
		t.Errorf("unexpected buffer message count: %d", count)
	}
	stats := buf.Stats()
	if stats.ActiveBatches != 1 {
		t.Errorf("unexpected stats active batches count: %d", stats.ActiveBatches)
	}
	if stats.ActiveMessages != 2 {
		t.Errorf("unexpected stats active messages count: %d", stats.ActiveMessages)
	}
	unpatch()

	unpatch = patchTime(time.Unix(1393650008, 0))
	if summaries := buf.Flush(false); len(summaries) != 0 {
		t.Errorf("unexpected summaries from flush: %v", summaries)
	}
	unpatch()

	unpatch = patchTime(time.Unix(1393650009, 0))
	summaries := buf.Flush(false)
	if len(summaries) != 1 {
		t.Errorf("unexpected summaries from flush: %v", summaries)
	}
	if count := buf.Stats().ActiveBatches; count != 0 {
		t.Errorf("unexpected buffer message count: %d", count)
	}
	if count := len(summaries[0].ReceivedMessages); count != 2 {
		t.Errorf("unexpected summary received message count: %d", count)
	}
	if count := len(summaries[0].UniqueMessages); count != 1 {
		t.Errorf("unexpected summary received unique message count: %d", count)
	}
	if subject := summaries[0].Subject; subject != "[failmail] 2 instances: test" {
		t.Errorf("unexpected summary subject: %s", subject)
	}

	stats = buf.Stats()
	if stats.ActiveBatches != 0 {
		t.Errorf("unexpected stats active batches count: %d", stats.ActiveBatches)
	}
	if stats.ActiveMessages != 0 {
		t.Errorf("unexpected stats active messages count: %d", stats.ActiveMessages)
	}
	unpatch()
}

func TestFlushForce(t *testing.T) {
	buf := NewMessageBuffer(5*time.Second, 9*time.Second, GroupByExpr("batch", `{{.Header.Get "Subject"}}`), GroupByExpr("group", `{{.Header.Get "Subject"}}`), NewMemoryStore(), "test@example.com")

	unpatch := patchTime(time.Unix(1393650000, 0))
	defer unpatch()
	buf.Add(makeReceivedMessage(t, "To: test@example.com\r\nSubject: test\r\n\r\ntest 1"))
	if count := buf.Stats().ActiveBatches; count != 1 {
		t.Errorf("unexpected buffer message count: %d", count)
	}
	unpatch()

	unpatch = patchTime(time.Unix(1393650004, 0))
	if summaries := buf.Flush(false); len(summaries) != 0 {
		t.Errorf("unexpected summaries from flush: %v", summaries)
	}
	if summaries := buf.Flush(true); len(summaries) != 1 {
		t.Errorf("unexpected summaries from flush: expected 1, got %d", len(summaries))
	}
	unpatch()
}

func TestRateCounter(t *testing.T) {
	r := NewRateCounter(10, 2)
	r.Add(5)
	exceeded, total := r.CheckAndAdvance()
	if exceeded {
		t.Errorf("rate limit should not be exceeded")
	}
	if total != 5 {
		t.Errorf("unexpected rate limit total: %d != 5", total)
	}

	r.Add(6)
	exceeded, total = r.CheckAndAdvance()
	if !exceeded {
		t.Errorf("rate limit should be exceeded")
	}
	if total != 11 {
		t.Errorf("unexpected rate limit total: %d != 11", total)
	}

	r.Add(1)
	exceeded, total = r.CheckAndAdvance()
	if exceeded {
		t.Errorf("rate limit should not be exceeded")
	}
	if total != 7 {
		t.Errorf("unexpected rate limit total: %d != 7", total)
	}
}

func TestDefaultFromAddress(t *testing.T) {
	defer patchHost("example.com", nil)()

	if from := DefaultFromAddress("test"); from != "test@example.com" {
		t.Errorf("unexpected from address: %s", from)
	}
}

func TestDefaultFromAddressHostnameError(t *testing.T) {
	defer patchHost("", fmt.Errorf("no hostname"))()

	if from := DefaultFromAddress("test"); from != "test@localhost" {
		t.Errorf("unexpected from address: %s", from)
	}
}

func TestPlural(t *testing.T) {
	if p := Plural(0, "message", "messages"); p != "0 messages" {
		t.Errorf("unexpected plural: %s", p)
	}
	if p := Plural(1, "message", "messages"); p != "1 message" {
		t.Errorf("unexpected plural: %s", p)
	}
	if p := Plural(11, "message", "messages"); p != "11 messages" {
		t.Errorf("unexpected plural: %s", p)
	}
}

func TestNormalizeAddress(t *testing.T) {
	if norm := NormalizeAddress("bad email address"); norm != "bad email address" {
		t.Errorf("unexpected normalization of invalid address: %s", norm)
	}

	if norm := NormalizeAddress("<TEST@example.com>"); norm != "test@example.com" {
		t.Errorf("unexpected normalization of address: %s", norm)
	}

	if norm := NormalizeAddress("Test User <test@example.com>"); norm != "test@example.com" {
		t.Errorf("unexpected normalization of address: %s", norm)
	}
}

func TestTemplateRenderer(t *testing.T) {
	defer patchTime(time.Date(2014, time.March, 1, 0, 0, 0, 0, time.UTC))()
	msg1 := makeReceivedMessage(t, "From: test@example.com\r\nTo: test2@example.com\r\nDate: Tue, 01 Jul 2014 12:34:56 -0400\r\nSubject: test\r\n\r\ntest body 1\r\n")
	msg2 := makeReceivedMessage(t, "From: test@example.com\r\nTo: test3@example.com\r\nDate: Wed, 02 Jul 2014 12:34:56 -0400\r\nSubject: test\r\n\r\ntest body 2\r\n")

	summarized := Summarize(GroupByExpr("group", `{{.Header.Get "Subject"}}`), "failmail@example.com", "test2@example.com", []*ReceivedMessage{msg1, msg2})

	templ := template.Must(template.New("summary").Parse("{{ range .UniqueMessages }}{{ .Count }} instances of {{ .Subject }}{{ end }}\n"))
	renderer := &TemplateRenderer{templ}
	rendered := renderer.Render(summarized)
	if contents := string(rendered.Contents()); contents != "2 instances of test\r\n" {
		t.Errorf("unexpected rendered message: %s", contents)
	}
}
