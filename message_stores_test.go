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
	if _, err := ds.Add(now, msg); err != nil {
		t.Errorf("failed to add message to store: %s", err)
	}

	if msgs, err := maildir.List(MAILDIR_CUR); err != nil {
		t.Errorf("error on maildir.List(): %s", err)
	} else if count := len(msgs); count != 1 {
		t.Errorf("unexpeted count for Maildir.List(), %d != 1", count)
	}

	newDs, err := NewDiskStore(maildir)
	if err != nil {
		t.Errorf("unexpected error creating new disk store: %s", err)
	}

	if msgs, err := newDs.MessagesNewerThan(time.Time{}); err != nil {
		t.Errorf("error on DiskStore.MessagesNewerThan(): %s", err)
	} else if count := len(msgs); count != 1 {
		t.Errorf("expected 1 message restored in new disk store, found %d", count)
	}
}
