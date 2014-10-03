// Support for zero-downtime reloading.
//
// A zero-downtime reload occurs roughly as follows:
//
// * On receipt of SIGUSR1, the signal handlers triggers a flush of message
//   buffers and shutdown of the sender (this process is identical to that used
//   for SIGTERM handling).
//
// * The SIGUSR1 handler also triggers shutdown of the listener, by closing
//   the listening socket that is blocking on `Accept()`. The listener sends a
//   reply back to the reloader when it's done, so the reloader can (later)
//   block until the listening socket is no longer being used. The reply
//   contains a file descriptor number that should be passed to the new
//   failmail process so that it can continue listening on the socket.
//
// * Before terminating, the reloader is consulted to see if a reload was
//   requested. If so, it blocks until it receives a file descriptor number
//   from the listener goroutine, and then executes a new failmail process,
//   passing it the same arguments it was invoked with, plus the file
//   descriptor it got from the listener.
//
// * The process exits, but the now detached child process continues,
//   inheriting the listening socket and opening it using the file descriptor
//   number passed on the command line.
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// Reloader holds references to channels used for processing a reload request,
// and tracks the state of whether a reload is necessary. The
// `ReloadIfNecessary()` method does the heavy lifting of spawning a new child
// process.
type Reloader struct {
	requests    chan bool
	replies     chan uintptr
	needsReload bool
}

func NewReloader() *Reloader {
	return &Reloader{make(chan bool, 1), make(chan uintptr, 1), false}
}

func (r *Reloader) RequestReload() {
	r.needsReload = true
	r.requests <- true
}

func (r *Reloader) OnRequest(getFd func() uintptr) {
	<-r.requests
	r.replies <- getFd()
}

// This should be called before shutting down, to check whether the program
// should invoke a new copy of itself (which will be given the listening TCP
// socket) before terminating, and to execute that new copy.
func (r *Reloader) ReloadIfNecessary() error {
	if !r.needsReload {
		return nil
	}

	fd := <-r.replies
	log.Printf("passing socket with fd %d", fd)

	// Remove socket-fd from args.
	args := make([]string, 0)
	consumeNextArg := false
	for _, arg := range os.Args[1:] {
		if !consumeNextArg && !strings.Contains(arg, "-socket-fd") {
			args = append(args, arg)
		} else if consumeNextArg {
			consumeNextArg = false
		} else if !strings.Contains(arg, "=") {
			consumeNextArg = true
		}
	}
	// The socket will always be fd 3 as long as it's ExtraFiles[0].
	args = append(args, fmt.Sprintf("--socket-fd=%d", 3))

	log.Printf("command: %s %#v\n", os.Args[0], args)
	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// If we don't put the fd in ExtraFiles, the child process gets a bad file
	// descriptor error when it tries to use the socket.
	cmd.ExtraFiles = []*os.File{os.NewFile(fd, "sock")}

	return cmd.Start()
}
