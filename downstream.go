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
	"reflect"
	"sync"
	"syscall"
	"time"
)

// Listener binds a socket on an address, and accepts email messages via SMTP
// on each incoming connection.
type Listener struct {
	*log.Logger
	Socket    ServerSocket
	Auth      Auth
	TLSConfig *tls.Config
	conns     int
	connLimit int
}

// ServerSocket is a `net.Listener` that can return its file descriptor.
type ServerSocket interface {
	net.Listener
	Fd() uintptr
	String() string
}

// TCPServerSocket is a ServerSocket implementation for listeners that bind a
// TCP port from an address.
type TCPServerSocket struct {
	*net.TCPListener
	addr string
}

func NewTCPServerSocket(addr string) (*TCPServerSocket, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}

	ln, err := net.ListenTCP("tcp", tcpAddr)
	return &TCPServerSocket{ln, addr}, err
}

func (t *TCPServerSocket) Fd() uintptr {
	return internalSocketFd(t.TCPListener)
}

func (t *TCPServerSocket) String() string {
	return t.addr
}

// FileServerSocket is a ServerSocket implementation for listeners that open an
// existing socket by its file descriptor.
type FileServerSocket struct {
	net.Listener
}

func NewFileServerSocket(fd uintptr) (*FileServerSocket, error) {
	file := os.NewFile(fd, "socket")
	ln, err := net.FileListener(file)
	syscall.Close(int(fd))
	return &FileServerSocket{ln}, err
}

func (f *FileServerSocket) Fd() uintptr {
	tcpListener := f.Listener.(*net.TCPListener)
	return internalSocketFd(tcpListener)
}

func (f *FileServerSocket) String() string {
	return fmt.Sprintf("fd %d", f.Fd())
}

func internalSocketFd(listener *net.TCPListener) uintptr {
	// This is not particularly robust, but TCPListener doesn't expose the FD
	// another way, and this implementation also serves as an assertion of
	// layout of the underlying data structure (meaning, we want to panic
	// rather than try to gracefully handle the error).
	return uintptr(reflect.ValueOf(listener).Elem().FieldByName("fd").Elem().FieldByName("sysfd").Int())
}

// Calls `Wait()` on a `sync.WaitGroup`, blocking for no more than the timeout.
// Returns true if the call to `Wait()` returned before hitting the timeout, or
// false otherwise.
func WaitWithTimeout(waitGroup *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan interface{}, 0)
	go func() {
		waitGroup.Wait()
		done <- nil
	}()

	timer := time.After(timeout)
	for {
		select {
		case <-timer:
			return false
		case <-done:
			return true
		}
	}
}

// Listens on a TCP port, putting all messages received via SMTP onto the
// `received` channel.
func (l *Listener) Listen(received chan<- *ReceivedMessage, reloader *Reloader, shutdownTimeout time.Duration) {
	l.Printf("listening: %s", l.Socket)
	waitGroup := new(sync.WaitGroup)

	go reloader.OnRequest(func() uintptr {
		l.Printf("waiting %s for open connections to finish", shutdownTimeout)
		WaitWithTimeout(waitGroup, shutdownTimeout)

		l.Printf("closing listening socket for reload")
		l.Socket.Close()
		fd := l.Socket.Fd()

		// If we don't dup the fd, the `syscall.Close()` that ends up happening
		// on it later will prevent us from being able to open it as a socket
		// in the child process.
		newfd, _ := syscall.Dup(int(fd))

		// If we don't mark the new fd as CLOEXEC, the child process will
		// inherit it twice (the second one being the one passed to
		// ExtraFiles).
		syscall.CloseOnExec(newfd)

		return uintptr(newfd)
	})

	for {
		conn, err := l.Socket.Accept()
		if err != nil {
			l.Printf("error accepting connection: %s", err)
			break
		}

		l.conns += 1

		// Handle each incoming connection in its own goroutine.
		l.Printf("handling new connection from %s", conn.RemoteAddr())
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			l.handleConnection(conn, received)
		}()

		if l.connLimit > 0 && l.conns >= l.connLimit {
			l.Printf("reached %d connections, stopping downstream listener", l.conns)
			break
		}
	}

	l.Printf("done listening")
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
				l.Printf("received message with subject %#v", msg.Parsed.Header.Get("Subject"))
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
