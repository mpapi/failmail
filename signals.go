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

func HandleSignals() chan TerminationRequest {
	signals := make(chan os.Signal, 1)
	reqs := make(chan TerminationRequest, 1)
	go func() {
		for sig := range signals {
			log.Printf("caught signal %s", sig)
			if sig == syscall.SIGUSR1 {
				reqs <- Reload
			} else {
				reqs <- GracefulShutdown
			}
		}
	}()
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGUSR1)
	return reqs
}
