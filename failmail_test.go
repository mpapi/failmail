package main

import (
	"io/ioutil"
	"os"
	"path"
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

func TestWritePidfile(t *testing.T) {
	testDir, cleanup := makeTestDir(t)
	defer cleanup()

	pidfile := path.Join(testDir, "test.pid")
	writePidfile(pidfile)
	if _, err := os.Stat(pidfile); err != nil && os.IsNotExist(err) {
		t.Errorf("no pidfile found at %s", pidfile)
	} else if err != nil && !os.IsNotExist(err) {
		t.Errorf("unexpected error looking for pidfile: %s", err)
	}
}

func makeTestDir(t *testing.T) (string, func()) {
	tmp, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("couldn't create temp dir: %s", err)
	}
	return tmp, func() { os.RemoveAll(tmp) }
}
