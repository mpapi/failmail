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
	"sync"
	"syscall"
	"time"
)

// Listener binds a socket on an address, and accepts email messages via SMTP
// on each incoming connection.
type Listener struct {
	Socket    ServerSocket
	Auth      Auth
	TLSConfig *tls.Config
	Debug     bool
	conns     int
}

// ServerSocket is a `net.Listener` that can return its file descriptor.
type ServerSocket interface {
	net.Listener
	Fd() (uintptr, error)
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

func (t *TCPServerSocket) Fd() (uintptr, error) {
	if file, err := t.File(); err != nil {
		return 0, err
	} else {
		return file.Fd(), nil
	}
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

func (f *FileServerSocket) Fd() (uintptr, error) {
	if tcpListener, ok := f.Listener.(*net.TCPListener); !ok {
		return 0, fmt.Errorf("%s is not a TCP socket", f)
	} else if file, err := tcpListener.File(); err != nil {
		return 0, err
	} else {
		return file.Fd(), err
	}
}

func (f *FileServerSocket) String() string {
	return fmt.Sprintf("fd from file")
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
func (l *Listener) Listen(received chan<- *StorageRequest, done <-chan TerminationRequest, shutdownTimeout time.Duration) (uintptr, error) {
	log.Printf("listening: %s", l.Socket)

	waitGroup := new(sync.WaitGroup)
	acceptFinished := make(chan bool, 0)

	// Accept connections in a goroutine, and add them to the WaitGroup.
	go func() {
		for {
			conn, err := l.Socket.Accept()
			if err != nil {
				log.Printf("error accepting connection: %s", err)
				break
			}

			l.conns += 1

			// Handle each incoming connection in its own goroutine.
			log.Printf("handling new connection from %s", conn.RemoteAddr())
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				l.handleConnection(conn, received)
				log.Printf("done handling new connection from %s", conn.RemoteAddr())
			}()
		}
		// When we've broken out of the loop for any reason (errors, limit),
		// signal that we're done via the channel.
		acceptFinished <- true
	}()

	newFd := 0

	// Wait for either a shutdown/reload request, or for the Accept() loop to
	// break on its own (from error or a limit).
	select {
	case req := <-done:

		// If we got a reload request, set up a file descriptor to pass to the
		// reloaded process.
		if req == Reload {
			fd, err := l.Socket.Fd()
			if err != nil {
				return 0, err
			}

			// If we don't dup the fd, closing it below (to break the Accept()
			// loop) will prevent us from being able to use it as a socket in
			// the child process.
			newFd, err = syscall.Dup(int(fd))
			if err != nil {
				return 0, err
			}

			// If we don't mark the new fd as CLOEXEC, the child process will
			// inherit it twice (the second one being the one passed to
			// ExtraFiles).
			syscall.CloseOnExec(newFd)
		}

		log.Printf("closing listening socket")
		if err := l.Socket.Close(); err != nil {
			return 0, err
		}

		// Wait for the Close() to break us out of the Accept() loop.
		<-acceptFinished

	case <-acceptFinished:
		// If the accept loop is done on its own (e.g. not from a reload
		// request), fall through to do some cleanup.
	}

	// Wait for any open sesssions to finish, or time out.
	log.Printf("waiting %s for open connections to finish", shutdownTimeout)
	WaitWithTimeout(waitGroup, shutdownTimeout)

	close(received)

	return uintptr(newFd), nil
}

// handleConnection reads SMTP commands from a socket and writes back SMTP
// responses. Since it takes several commands (MAIL, RCPT, DATA) to fully
// describe a message, `Session` is used to keep track of the progress building
// a message. When a message has been fully communicated by a downstream
// client, it's put on the `received` channel for later batching/summarizing.
func (l *Listener) handleConnection(conn io.ReadWriteCloser, received chan<- *StorageRequest) {
	defer conn.Close()

	origReader := bufio.NewReader(conn)
	origWriter := bufio.NewWriter(conn)

	// In debug mode, wrap the readers and writers.
	var reader stringReader
	var writer stringWriter
	if l.Debug {
		prefix := fmt.Sprintf("%v ", conn)
		reader = &debugReader{origReader, prefix}
		writer = &debugWriter{origWriter, prefix}
	} else {
		reader = origReader
		writer = origWriter
	}

	session := new(Session)
	if err := session.Start(l.Auth, l.TLSConfig != nil).WriteTo(writer); err != nil {
		log.Printf("error writing to client: %s", err)
		return
	}

	for {
		resp, err := session.ReadCommand(reader)
		if err != nil {
			log.Printf("error reading from client: %s", err)
			break
		}

		if err := resp.WriteTo(writer); err != nil {
			log.Printf("error writing to client after reading command: %s", err)
			break
		}

		switch {
		case resp.IsClose():
			return
		case resp.NeedsData():
			resp, msg := session.ReadData(reader)
			if msg != nil {
				log.Printf("received message with subject %#v", msg.Parsed.Header.Get("Subject"))
				errors := make(chan error, 0)
				received <- &StorageRequest{msg, errors}
				if err := <-errors; err != nil {
					errorResp := Response{451, err.Error()}
					if err := errorResp.WriteTo(writer); err != nil {
						log.Printf("error writing to client after storage failure: %s", err)
						break
					}
				} else {
					if err := resp.WriteTo(writer); err != nil {
						log.Printf("error writing to client after reading data: %s", err)
						break
					}
				}
			}
		case resp.NeedsAuthResponse():
			resp := session.ReadAuthResponse(reader)
			if err := resp.WriteTo(writer); err != nil {
				log.Printf("error writing to client after reading auth: %s", err)
				break
			}
		case resp.StartsTLS():
			netConn, ok := conn.(net.Conn)
			if !ok {
				log.Printf("error getting underlying connection for STARTTLS")
				return
			}
			tlsConn := tls.Server(netConn, l.TLSConfig)
			origReader.Reset(tlsConn)
			origWriter.Reset(tlsConn)
			defer tlsConn.Close()
		}
	}
}
