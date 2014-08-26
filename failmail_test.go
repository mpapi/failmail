package main

import (
	"testing"
)

func TestBatchConfig(t *testing.T) {
	msg := makeReceivedMessage(t, "Subject: that test\r\nX-Batch: 100\r\n\r\ntest body\r\n")

	batch := (&Config{BatchMatch: "^(this|that)", BatchReplace: "^(this|that)", BatchHeader: "X-Batch"}).Batch()
	if key := batch(msg); key != "that" {
		t.Errorf("expected message batch 'that', got %#v", key)
	}

	batch = (&Config{BatchMatch: "", BatchReplace: "^(this|that)", BatchHeader: "X-Batch"}).Batch()
	if key := batch(msg); key != "* test" {
		t.Errorf("expected message batch '* test', got %#v", key)
	}

	batch = (&Config{BatchMatch: "", BatchReplace: "", BatchHeader: "X-Batch"}).Batch()
	if key := batch(msg); key != "100" {
		t.Errorf("expected message batch '100', got %#v", key)
	}
}

func TestGroupConfig(t *testing.T) {
	msg := makeReceivedMessage(t, "Subject: that test\r\nX-Batch: 100\r\n\r\ntest body\r\n")

	group := (&Config{GroupMatch: "^(this|that)", GroupReplace: "^(this|that)"}).Group()
	if key := group(msg); key != "that" {
		t.Errorf("expected message group 'that', got %#v", key)
	}

	group = (&Config{GroupMatch: "", GroupReplace: "^(this|that)"}).Group()
	if key := group(msg); key != "* test" {
		t.Errorf("expected message group '* test', got %#v", key)
	}

	group = (&Config{GroupMatch: "", GroupReplace: ""}).Group()
	if key := group(msg); key != "that test" {
		t.Errorf("expected message group 'that test', got %#v", key)
	}
}
