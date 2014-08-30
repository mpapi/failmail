// Implementations for receiving incoming email messages and placing them them
// on a sendable channel for batching/summarizing/processing.
package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

// Listener binds a socket on an address, and accepts email messages via SMTP
// on each incoming connection.
type Listener struct {
	*log.Logger
	Creator   socketCreator
	Auth      Auth
	TLSConfig *tls.Config
	conns     int
	connLimit int
}

type socketCreator interface {
	Listen() (net.Listener, error)
	String() string
}

type tcpSocketCreator struct {
	Addr string
}

func (t *tcpSocketCreator) Listen() (net.Listener, error) {
	return net.Listen("tcp", t.Addr)
}

func (t *tcpSocketCreator) String() string {
	return t.Addr
}

type fileSocketCreator struct {
	Fd uintptr
}

func (f *fileSocketCreator) Listen() (net.Listener, error) {
	file := os.NewFile(f.Fd, "socket")
	return net.FileListener(file)
}

func (f *fileSocketCreator) String() string {
	return fmt.Sprintf("fd %d", f.Fd)
}

// Listens on a TCP port, putting all messages received via SMTP onto the
// `received` channel.
func (l *Listener) Listen(received chan<- *ReceivedMessage) {
	l.Printf("listening: %s", l.Creator)
	ln, err := l.Creator.Listen()
	if err != nil {
		l.Fatalf("error starting listener: %s", err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			l.Printf("error accepting connection: %s", err)
			continue
		}

		l.conns += 1

		// Handle each incoming connection in its own goroutine.
		l.Printf("handling new connection from %s", conn.RemoteAddr())
		go l.handleConnection(conn, received)

		if l.connLimit > 0 && l.conns >= l.connLimit {
			l.Printf("reached %d connections, stopping downstream listener", l.conns)
			break
		}
	}
}

// handleConnection reads SMTP commands from a socket and writes back SMTP
// responses. Since it takes several commands (MAIL, RCPT, DATA) to fully
// describe a message, `Session` is used to keep track of the progress building
// a message. When a message has been fully communicated by a downstream
// client, it's put on the `received` channel for later batching/summarizing.
func (l *Listener) handleConnection(conn io.ReadWriteCloser, received chan<- *ReceivedMessage) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	session := new(Session)
	session.Start(l.Auth, l.TLSConfig != nil).WriteTo(writer)

	for {
		resp, err := session.ReadCommand(reader)
		if err != nil {
			l.Printf("error reading from client: %s", err)
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
				l.Printf("received message with subject %#v", msg.Message.Header.Get("Subject"))
				received <- msg
			}
		case resp.NeedsAuthResponse():
			resp := session.ReadAuthResponse(reader)
			resp.WriteTo(writer)
		case resp.StartsTLS():
			netConn, ok := conn.(net.Conn)
			if !ok {
				l.Printf("error getting underlying connection for STARTTLS")
				return
			}
			tlsConn := tls.Server(netConn, l.TLSConfig)
			reader.Reset(tlsConn)
			writer.Reset(tlsConn)
			defer tlsConn.Close()
		}
	}
}
