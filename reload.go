package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
)

type Reloader struct {
	cleanupRequests chan<- bool
	requests        chan bool
	replies         chan uintptr
	needsReload     bool
}

func NewReloader(cleanupRequests chan<- bool) *Reloader {
	return &Reloader{cleanupRequests, make(chan bool, 1), make(chan uintptr, 1), false}
}

func (r *Reloader) HandleSignals() {
	reloadSig := make(chan os.Signal, 1)
	signal.Notify(reloadSig, syscall.SIGUSR1)

	for sig := range reloadSig {
		log.Printf("caught signal %s for reload", sig)
		r.needsReload = true
		r.cleanupRequests <- true
		r.requests <- true
	}
}

func (r *Reloader) OnRequest(getFd func() uintptr) {
	<-r.requests
	r.replies <- getFd()
}

func (r *Reloader) ReloadIfNecessary() {
	if !r.needsReload {
		return
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
	args = append(args, fmt.Sprintf("--socket-fd=%d", 3))

	log.Printf("command: %s %#v\n", os.Args[0], args)
	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{os.NewFile(fd, "sock")}
	if err := cmd.Start(); err != nil {
		log.Fatalf("failed to start new proc: %s", err)

	}
}
