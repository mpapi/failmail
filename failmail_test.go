package main

import (
	"testing"
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
