package main

import (
	"errors"
	"testing"
	"time"
)

func TestBatchConfig(t *testing.T) {
	msg := makeReceivedMessage(t, "Subject: that test\r\nX-Batch: 100\r\n\r\ntest body\r\n")

	batch := (&Config{BatchExpr: `{{match "^(this|that)" (.Header.Get "Subject")}}`}).Batch()
	if key, err := batch(msg); key != "that" || err != nil {
		t.Errorf("expected message batch 'that', got %#v, %s", key, err)
	}

	batch = (&Config{BatchExpr: `{{replace "^(this|that)" (.Header.Get "Subject") "*"}}`}).Batch()
	if key, err := batch(msg); key != "* test" || err != nil {
		t.Errorf("expected message batch '* test', got %#v, %s", key, err)
	}

	batch = (&Config{BatchExpr: `{{.Header.Get "X-Batch"}}`}).Batch()
	if key, err := batch(msg); key != "100" || err != nil {
		t.Errorf("expected message batch '100', got %#v, %s", key, err)
	}
}

func TestGroupConfig(t *testing.T) {
	msg := makeReceivedMessage(t, "Subject: that test\r\nX-Batch: 100\r\n\r\ntest body\r\n")

	group := (&Config{GroupExpr: `{{match "^(this|that)" (.Header.Get "Subject")}}`}).Group()
	if key, err := group(msg); key != "that" || err != nil {
		t.Errorf("expected message group 'that', got %#v, %s", key, err)
	}

	group = (&Config{GroupExpr: `{{replace "^(this|that)" (.Header.Get "Subject") "*"}}`}).Group()
	if key, err := group(msg); key != "* test" || err != nil {
		t.Errorf("expected message group '* test', got %#v, %s", key, err)
	}
}

type TestUpstream struct {
	Sends       []OutgoingMessage
	ReturnError error
}

func (t *TestUpstream) Send(msg OutgoingMessage) error {
	if t.ReturnError != nil {
		return t.ReturnError
	}
	t.Sends = append(t.Sends, msg)
	return nil
}

func TestSendUpstream(t *testing.T) {
	failedMaildir, cleanup := makeTestMaildir(t)
	defer cleanup()

	upstream := &TestUpstream{make([]OutgoingMessage, 0), nil}
	outgoing := make(chan OutgoingMessage, 0)

	done := make(chan bool, 0)
	go func() {
		sendUpstream(outgoing, upstream, failedMaildir)
		done <- true
	}()

	outgoing <- &message{"test", []string{"test"}, []byte("test")}
	close(outgoing)

	if count := len(upstream.Sends); count != 1 {
		t.Errorf("expected one successful upstream send, got %d", count)
	}

	msgs, err := failedMaildir.List()
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

func TestSendUpstreamFailed(t *testing.T) {
	failedMaildir, cleanup := makeTestMaildir(t)
	defer cleanup()

	upstream := &TestUpstream{make([]OutgoingMessage, 0), errors.New("fail")}
	outgoing := make(chan OutgoingMessage, 0)

	done := make(chan bool, 0)
	go func() {
		sendUpstream(outgoing, upstream, failedMaildir)
		done <- true
	}()

	outgoing <- &message{"test", []string{"test"}, []byte("test")}
	close(outgoing)

	if count := len(upstream.Sends); count != 0 {
		t.Errorf("expected one successful upstream send, got %d", count)
	}

	msgs, err := failedMaildir.List()
	if err != nil {
		t.Errorf("unexpected error listing maildir for failed messages: %s", err)
	} else if count := len(msgs); count != 1 {
		t.Errorf("expected no messages in failed maildir, got %d", count)
	}

	select {
	case <-time.Tick(100 * time.Millisecond):
		t.Fatalf("timed out")
	case <-done:
	}
}
