package main

import (
	"io/ioutil"
	"log"
	"net/textproto"
	"testing"
)

var testLogger = log.New(ioutil.Discard, "", log.LstdFlags)

func TestListener(t *testing.T) {
	listener := &Listener{Logger: testLogger, Addr: "localhost:40025", connLimit: 1}
	received := make(chan *ReceivedMessage, 1)
	done := make(chan bool, 0)

	go func() {
		conn, err := textproto.Dial("tcp", "localhost:40025")
		if err != nil {
			t.Fatalf("failed to connect to listener: %s", err)
		}

		_, _, err = conn.ReadCodeLine(220)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		err = conn.PrintfLine("QUIT")
		if err != nil {
			t.Errorf("unexpected error writing to server: %s", err)
		}

		_, _, err = conn.ReadCodeLine(221)
		if err != nil {
			t.Errorf("unexpected response from server: %s", err)
		}

		err = conn.Close()
		if err != nil {
			t.Errorf("failed to close listener: %s", err)
		}

		done <- true
	}()

	listener.Listen(received)
	<-done
}
