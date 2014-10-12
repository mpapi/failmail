package main

import (
	"testing"
	"time"
)

func TestDiskStore(t *testing.T) {
	maildir, cleanup := makeTestMaildir(t)
	defer cleanup()

	ds, err := NewDiskStore(maildir)
	if err != nil {
		t.Fatalf("couldn't create disk store: %s", err)
	}

	msg := makeReceivedMessage(t, "From: test@example.com\r\nTo: test@example.com\r\nSubject: test\r\n\r\ntest\r\n")
	now := time.Unix(1393650000, 0)
	if err := ds.Add(now, RecipientKey{"test", "test@example.com"}, msg); err != nil {
		t.Errorf("failed to add message to store: %s", err)
	}

	results := make([]*ReceivedMessage, 0)
	calls := 0
	callback := func(key RecipientKey, loadFunc func() []*ReceivedMessage, softLimit time.Time, hardLimit time.Time) bool {
		calls += 1
		for _, msg := range loadFunc() {
			results = append(results, msg)
		}
		return true
	}
	if err := ds.Iterate(callback); err != nil {
		t.Errorf("error on Iterate(): %s", err)
	} else if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	} else if count := len(results); count != 1 {
		t.Errorf("expected 1 message, got %d", count)
	}

	if msgs, err := maildir.List(); err != nil {
		t.Errorf("error on maildir.List(): %s", err)
	} else if count := len(msgs); count != 0 {
		t.Errorf("expected empty maildir, found %d entries", count)
	}

	// Add the message back.
	if err := ds.Add(now, RecipientKey{"test", "test@example.com"}, msg); err != nil {
		t.Errorf("failed to add message to store: %s", err)
	}

	// Make sure it was written.
	if msgs, err := maildir.List(); err != nil {
		t.Errorf("error on maildir.List(): %s", err)
	} else if count := len(msgs); count != 2 {
		t.Errorf("expected 2 entries in maildir, found %d", count)
	}

	newDs, err := NewDiskStore(maildir)
	if err != nil {
		t.Errorf("unexpected error creating new disk store: %s", err)
	}

	if count := len(newDs.messages); count != 1 {
		t.Errorf("expected 1 message restored in new disk store, found %d", count)
	}
}
