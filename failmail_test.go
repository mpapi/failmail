package main

import (
	"testing"
)

func TestBatchConfig(t *testing.T) {
	msg := makeReceivedMessage(t, "Subject: that test\r\nX-Batch: 100\r\n\r\ntest body\r\n")

	batch := buildBatch("^(this|that)", "^(this|that)", "X-Batch")
	if key := batch(msg); key != "that" {
		t.Errorf("expected message batch 'that', got %#v", key)
	}

	batch = buildBatch("", "^(this|that)", "X-Batch")
	if key := batch(msg); key != "* test" {
		t.Errorf("expected message batch '* test', got %#v", key)
	}

	batch = buildBatch("", "", "X-Batch")
	if key := batch(msg); key != "100" {
		t.Errorf("expected message batch '100', got %#v", key)
	}
}

func TestGroupConfig(t *testing.T) {
	msg := makeReceivedMessage(t, "Subject: that test\r\nX-Batch: 100\r\n\r\ntest body\r\n")

	group := buildGroup("^(this|that)", "^(this|that)")
	if key := group(msg); key != "that" {
		t.Errorf("expected message group 'that', got %#v", key)
	}

	group = buildGroup("", "^(this|that)")
	if key := group(msg); key != "* test" {
		t.Errorf("expected message group '* test', got %#v", key)
	}

	group = buildGroup("", "")
	if key := group(msg); key != "that test" {
		t.Errorf("expected message group 'that test', got %#v", key)
	}
}
