package main

import (
	"fmt"
	"testing"
	"time"
)

func TestReceivedMessageBody(t *testing.T) {
	msg := makeReceivedMessage(t, "Subject: test\r\n\r\ntest body\r\n")
	if body := msg.Body(); body != "test body\r\n" {
		t.Errorf("unexpected message body: %s", body)
	}
	if body := msg.Body(); body != "test body\r\n" {
		t.Errorf("unexpected message body on 2nd call: %s", body)
	}
}

func TestMessageBuffer(t *testing.T) {
	buf := NewMessageBuffer(5*time.Second, 9*time.Second, SameSubject(), SameSubject())

	unpatch := patchTime(time.Unix(1393650000, 0))
	defer unpatch()
	buf.Add(makeReceivedMessage(t, "Subject: test\r\n\r\ntest 1"))
	if count := len(buf.messages); count != 1 {
		t.Errorf("unexpected buffer message count: %d", count)
	}
	unpatch()

	unpatch = patchTime(time.Unix(1393650004, 0))
	if summaries := buf.Flush("test@example.com"); len(summaries) != 0 {
		t.Errorf("unexpected summaries from flush: %s", summaries)
	}
	unpatch()

	unpatch = patchTime(time.Unix(1393650005, 0))
	buf.Add(makeReceivedMessage(t, "Subject: test\r\n\r\ntest 2"))
	if count := len(buf.messages); count != 1 {
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
	if summaries := buf.Flush("test@example.com"); len(summaries) != 0 {
		t.Errorf("unexpected summaries from flush: %s", summaries)
	}
	unpatch()

	unpatch = patchTime(time.Unix(1393650009, 0))
	summaries := buf.Flush("test@example.com")
	if len(summaries) != 1 {
		t.Errorf("unexpected summaries from flush: %s", summaries)
	}
	if count := len(buf.messages); count != 0 {
		t.Errorf("unexpected buffer message count: %d", count)
	}
	if count := len(summaries[0].ReceivedMessages); count != 2 {
		t.Errorf("unexpected summary received message count: %d", count)
	}
	if count := len(summaries[0].UniqueMessages); count != 1 {
		t.Errorf("unexpected summary received unique message count: %d", count)
	}
	if subject := summaries[0].Subject; subject != "[failmail] 2 messages" {
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

func TestReplacedSubject(t *testing.T) {
	groupBy := ReplacedSubject(`\d+`, "#")
	key := groupBy(makeReceivedMessage(t, "Subject: message 1 of 20\r\n\r\ntest\r\n"))
	if key != "message # of #" {
		t.Errorf("unexpected grouping: %s", key)
	}
}

func TestMatchingSubject(t *testing.T) {
	groupBy := MatchingSubject(`^myapp: `)
	key := groupBy(makeReceivedMessage(t, "Subject: myapp: error in foo\r\n\r\ntest\r\n"))
	if key != "myapp: " {
		t.Errorf("unexpected grouping: %s", key)
	}
}

func TestSameSubject(t *testing.T) {
	groupBy := SameSubject()
	key := groupBy(makeReceivedMessage(t, "Subject: test\r\n\r\ntest\r\n"))
	if key != "test" {
		t.Errorf("unexpected grouping: %s", key)
	}
}

func TestHeader(t *testing.T) {
	groupBy := Header("X-Failmail", "default")
	key := groupBy(makeReceivedMessage(t, "Subject: test\r\nX-Failmail: errors\r\n\r\ntest\r\n"))
	if key != "errors" {
		t.Errorf("unexpected grouping: %s", key)
	}
}

func TestHeaderDefault(t *testing.T) {
	groupBy := Header("X-Failmail", "default")
	key := groupBy(makeReceivedMessage(t, "Subject: test\r\n\r\ntest\r\n"))
	if key != "default" {
		t.Errorf("unexpected grouping: %s", key)
	}
}
