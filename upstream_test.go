package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"net/mail"
	"path"
	"testing"
	"time"
)

const (
	TEST_MESSAGE = `From: test@example.com
To: test@example.com
Subject: test

test message
`
)

type errorUpstream struct {
	Error error
}

func (u *errorUpstream) Send(m OutgoingMessage) error {
	return u.Error
}

func TestDebugUpstream(t *testing.T) {
	summary := makeSummaryMessage(t, TEST_MESSAGE)

	buf := new(bytes.Buffer)
	upstream := DebugUpstream{buf}
	err := upstream.Send(summary)
	if err != nil {
		t.Errorf("failed to send message: %s", err)
	}

	sent, err := mail.ReadMessage(buf)
	if err != nil {
		t.Errorf("failed to parse sent message: %s", err)
	}

	if subject := sent.Header.Get("Subject"); subject != "test" {
		t.Errorf("unexpected subject for sent message: %s", subject)
	}
}

func TestMultiUpstream(t *testing.T) {
	summary := makeSummaryMessage(t, TEST_MESSAGE)

	buf1 := new(bytes.Buffer)
	buf2 := new(bytes.Buffer)
	upstream := NewMultiUpstream(&DebugUpstream{buf1}, &DebugUpstream{buf2})

	err := upstream.Send(summary)
	if err != nil {
		t.Errorf("failed to send message: %s", err)
	}

	for i, buf := range []*bytes.Buffer{buf1, buf2} {
		sent, err := mail.ReadMessage(buf)
		if err != nil {
			t.Errorf("failed to parse sent message %d: %s", i, err)
		}

		if subject := sent.Header.Get("Subject"); subject != "test" {
			t.Errorf("unexpected subject for sent message %d: %s", i, subject)
		}
	}
}

func TestMultiUpstreamError(t *testing.T) {
	summary := makeSummaryMessage(t, TEST_MESSAGE)

	buf := new(bytes.Buffer)
	upstream := NewMultiUpstream(&errorUpstream{fmt.Errorf("error")}, &DebugUpstream{buf})

	err := upstream.Send(summary)
	if err == nil {
		t.Errorf("expected error sending message")
	}

	if len(buf.Bytes()) != 0 {
		t.Errorf("unexpected data in buffer: %s", string(buf.Bytes()))
	}
}

func TestMaildirUpstream(t *testing.T) {
	summary := makeSummaryMessage(t, TEST_MESSAGE)

	maildir, cleanup := makeTestMaildir(t)
	defer cleanup()

	upstream := &MaildirUpstream{maildir}

	err := upstream.Send(summary)
	if err != nil {
		t.Errorf("failed to send message: %s", err)
	}

	if entries, err := ioutil.ReadDir(path.Join(maildir.Path, "cur")); err != nil {
		t.Fatalf("couldn't read maildir: %s", err)
	} else if len(entries) != 1 {
		t.Fatalf("unexpected maildir contents: %v", entries)
	} else if data, err := ioutil.ReadFile(path.Join(maildir.Path, "cur", entries[0].Name())); err != nil {
		t.Fatalf("couldn't read message in maildir: %s", err)
	} else {
		sent, err := mail.ReadMessage(bytes.NewBuffer(data))
		if err != nil {
			t.Errorf("couldn't parse message in maildir: %s", err)
		}
		if subject := sent.Header.Get("Subject"); subject != "test" {
			t.Errorf("unexpected subject: %s", subject)
		}
	}
}

func TestSender(t *testing.T) {
	failedMaildir, cleanup := makeTestMaildir(t)
	defer cleanup()

	upstream := &TestUpstream{make([]OutgoingMessage, 0), nil}
	outgoing := make(chan *SendRequest, 0)

	done := make(chan bool, 0)
	go func() {
		sender := &Sender{upstream, failedMaildir}
		sender.Run(outgoing)
		done <- true
	}()

	errors := make(chan error, 0)
	outgoing <- &SendRequest{&message{"test", []string{"test"}, []byte("test")}, errors}
	<-errors
	close(outgoing)

	if count := len(upstream.Sends); count != 1 {
		t.Errorf("expected one successful upstream send, got %d", count)
	}

	msgs, err := failedMaildir.List(MAILDIR_CUR)
	if err != nil {
		t.Errorf("unexpected error listing maildir for failed messages: %s", err)
	} else if count := len(msgs); count != 0 {
		t.Errorf("expected no messages in failed maildir, got %d", count)
	}

	select {
	case <-time.Tick(100 * time.Millisecond):
		t.Fatalf("timed out")
	case <-done:
	}
}

func TestSenderFailed(t *testing.T) {
	failedMaildir, cleanup := makeTestMaildir(t)
	defer cleanup()

	upstream := &TestUpstream{make([]OutgoingMessage, 0), errors.New("fail")}
	outgoing := make(chan *SendRequest, 0)

	done := make(chan bool, 0)
	go func() {
		sender := &Sender{upstream, failedMaildir}
		sender.Run(outgoing)
		done <- true
	}()

	errors := make(chan error, 0)
	outgoing <- &SendRequest{&message{"test", []string{"test"}, []byte("test")}, errors}
	<-errors
	close(outgoing)

	select {
	case <-time.Tick(100 * time.Millisecond):
		t.Fatalf("timed out")
	case <-done:
	}

	if count := len(upstream.Sends); count != 0 {
		t.Errorf("expected one successful upstream send, got %d", count)
	}

	msgs, err := failedMaildir.List(MAILDIR_CUR)
	if err != nil {
		t.Errorf("unexpected error listing maildir for failed messages: %s", err)
	} else if count := len(msgs); count != 1 {
		t.Errorf("expected no messages in failed maildir, got %d", count)
	}
}

func makeReceivedMessage(t *testing.T, data string) *ReceivedMessage {
	buf := bytes.NewBufferString(data)
	msg, err := mail.ReadMessage(buf)
	if err != nil {
		t.Fatalf("failed to build message: %s", err)
	}

	return &ReceivedMessage{
		message: &message{msg.Header.Get("From"), msg.Header["To"], []byte(data)},
		Parsed:  msg,
	}
}

func makeSummaryMessage(t *testing.T, data ...string) *SummaryMessage {
	msgs := make([]*ReceivedMessage, 0)
	for _, d := range data {
		msgs = append(msgs, makeReceivedMessage(t, d))
	}
	stored := makeStoredMessages(msgs...)
	compacted, err := Compact(GroupByExpr("group", `{{.Header.Get "Subject"}}`), stored)
	if err != nil {
		t.Fatalf("error in Compact(): %s", err)
	}

	return &SummaryMessage{
		From:           "test@example.com",
		To:             []string{"test@example.com"},
		Subject:        "test",
		Date:           time.Now(),
		StoredMessages: stored,
		UniqueMessages: compacted,
	}
}
