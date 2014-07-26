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

func TestListenerWithMessage(t *testing.T) {
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

		sendAndExpect(conn, t, "HELO localhost", 250)
		sendAndExpect(conn, t, "MAIL FROM:<test@localhost>", 250)
		sendAndExpect(conn, t, "RCPT TO:<test@localhost>", 250)
		sendAndExpect(conn, t, "DATA", 354)
		sendAndExpect(conn, t, "Subject: test\r\n\r\nbody\r\n.", 250)
		sendAndExpect(conn, t, "QUIT", 221)

		err = conn.Close()
		if err != nil {
			t.Errorf("failed to close listener: %s", err)
		}

		done <- true
	}()

	listener.Listen(received)
	<-done
}

func sendAndExpect(conn *textproto.Conn, t *testing.T, line string, code int) {
	err := conn.PrintfLine(line)
	if err != nil {
		t.Errorf("unexpected error writing to server: %s", err)
	}

	_, _, err = conn.ReadCodeLine(code)
	if err != nil {
		t.Errorf("unexpected response from server: %s", err)
	}
}
