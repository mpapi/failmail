package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

type TerminationRequest int

const (
	GracefulShutdown TerminationRequest = iota
	Reload
)

func HandleSignals(reqs chan<- TerminationRequest) {
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGUSR1)
	for sig := range signals {
		log.Printf("caught signal %s", sig)
		if sig == syscall.SIGUSR1 {
			reqs <- Reload
		} else {
			reqs <- GracefulShutdown
		}
	}
}
