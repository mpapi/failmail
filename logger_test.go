package main

import (
	"io/ioutil"
	"os"
	"strings"
	"testing"
)

func TestLogger(t *testing.T) {
	output, cleanup := makeTestLogFile(t)
	defer cleanup()
	defer patchLoggerWriter(output)()

	l := logger("test")
	l.Printf("log")

	if _, err := output.Seek(0, 0); err != nil {
		t.Fatalf("couldn't seek temp file: %s", err)
	}
	contents, err := ioutil.ReadAll(output)
	if err != nil {
		t.Fatalf("couldn't read temp file: %s", err)
	}

	if !strings.HasPrefix(string(contents), "[test]") {
		t.Errorf("unexpected log output: %s", string(contents))
	} else if !strings.HasSuffix(string(contents), "log\n") {
		t.Errorf("unexpected log output: %s", string(contents))
	}
}

func makeTestLogFile(t *testing.T) (*os.File, func()) {
	tmp, err := ioutil.TempFile("", "log")
	if err != nil {
		t.Fatalf("couldn't create temp file: %s", err)
	}

	return tmp, func() { os.Remove(tmp.Name()) }
}

func patchLoggerWriter(out *os.File) func() {
	orig := loggerWriter
	loggerWriter = out
	return func() { loggerWriter = orig }
}
