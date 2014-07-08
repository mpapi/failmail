package main

import (
	"bufio"
	"io"
	"log"
	"net"
)

type Listener struct {
	*log.Logger
	Addr string
}

// Listens on a TCP port, putting all messages received via SMTP onto the
// `received` channel.
func (l *Listener) Listen(received chan<- *ReceivedMessage) {
	l.Printf("listening: %s", l.Addr)
	ln, err := net.Listen("tcp", l.Addr)
	if err != nil {
		l.Fatalf("error starting listener: %s", err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			l.Printf("error accepting connection: %s", err)
			continue
		}

		l.Printf("handling new connection: %s", conn)
		go l.handleConnection(conn, received)
	}
}

func (l *Listener) handleConnection(conn io.ReadWriteCloser, received chan<- *ReceivedMessage) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	session := new(Session)
	session.Start().WriteTo(writer)

	for {
		resp, err := session.ReadCommand(reader)
		if err != nil {
			l.Printf("error reading from client:", err)
			break
		}

		resp.WriteTo(writer)

		switch {
		case resp.IsClose():
			return
		case resp.NeedsData():
			resp, msg := session.ReadData(reader)
			resp.WriteTo(writer)
			if msg != nil {
				received <- msg
			}
		}
	}
}
