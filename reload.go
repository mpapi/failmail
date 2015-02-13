// Support for zero-downtime reloading.
//
// A zero-downtime reload occurs roughly as follows:
//
// * On receipt of SIGUSR1 or SIGTERM, the signal handler triggers
//   graceful shutdown of message handling goroutines (waiting until messages
//   in flight are committed to storage or summarized and sent).
//
// * On shutdown, the listener returns file descriptor that should be passed to
//   a new failmail process so that it can continue listening on the socket.
//   Some system calls are made to ensure that that file descriptor (and no
//   others) are in the right state for seamless inheritance by the child
//   process.
//
// * If necessary, `TryReload` is called with the file descriptor returned by
//   the listener, which executes a new failmail process, passing it the same
//   arguments it was invoked with, plus the file descriptor it got from the
//   listener.
//
// * The parent process exits, but the now detached child process continues,
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

// This should be called before shutting down, to check whether the program
// should invoke a new copy of itself (which will be given the listening TCP
// socket) before terminating, and to execute that new copy.
func TryReload(shouldReload bool, fd uintptr) error {
	if !shouldReload {
		return nil
	}

	if fd == 0 {
		return fmt.Errorf("reload requested but socket fd was 0")
	}

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
