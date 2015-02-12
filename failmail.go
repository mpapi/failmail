package main

import (
	"fmt"
	"github.com/hut8labs/failmail/configure"
	"log"
	"os"
	"sync"
)

const VERSION = "0.2.0"

const LOGO = `
     *===================*
    / .-----------------. \
   /  |                 |  \
  +\~~|       :(        |~~/+
  | \_._________________._/ |
  |  /                   \  |
  | /   failmail v%5s   \ |
  |/_______________________\|
`

func main() {
	config := Defaults()

	wroteConfig, err := configure.Parse(config, fmt.Sprintf(LOGO, VERSION))
	if err != nil {
		log.Fatalf("Failed to read configuration: %s", err)
	} else if wroteConfig {
		return
	}

	if config.Version {
		fmt.Fprintf(os.Stderr, "failmail %s\n", VERSION)
		return
	}
	log.Printf("failmail %s, starting up", VERSION)

	if config.Pidfile != "" {
		writePidfile(config.Pidfile)
		defer os.Remove(config.Pidfile)
	}

	reloader := NewReloader()

	signalListeners := make([]chan<- TerminationRequest, 0)
	waitGroup := new(sync.WaitGroup)

	if config.Receiver {
		listener, err := config.Listener()
		if err != nil {
			log.Fatalf("failed to create listener: %s", err)
		}

		writer, err := config.Writer()
		if err != nil {
			log.Fatalf("failed to create writer: %s", err)
		}

		// A channel for incoming messages. The listener sends on the channel, and
		// receives are added to a MessageBuffer in the channel consumer below.
		received := make(chan *StorageRequest, 64)

		done := make(chan TerminationRequest, 1)
		signalListeners = append(signalListeners, done)

		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			listener.Listen(received, done, reloader, config.ShutdownTimeout)
			log.Printf("receiver: done")
		}()

		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			writer.Run(received)
			log.Printf("writer: done")
		}()
	}

	if config.Sender {
		// A `MessageBuffer` collects incoming messages and decides how to batch
		// them up and when to relay them to an upstream SMTP server.
		buffer, err := config.Buffer()
		if err != nil {
			log.Fatalf("failed to create buffer: %s", err)
		}

		sender, err := config.MakeSender()
		if err != nil {
			log.Fatalf("failed to create sender: %s", err)
		}

		go ListenHTTP(config.BindHTTP, buffer)

		// A channel for outgoing messages.
		outgoing := make(chan *SendRequest, 64)

		done := make(chan TerminationRequest, 1)
		signalListeners = append(signalListeners, done)

		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			buffer.Run(outgoing, done)
			log.Printf("summarizer: done")
		}()

		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			sender.Run(outgoing)
			log.Printf("sender: done")
		}()
	}

	if !config.Receiver && !config.Sender {
		log.Fatalf("must specify --receiver and/or --sender")
	}

	// Handle signals for reloading/shutdown, wait for goroutines to finish.
	HandleSignals(signalListeners)
	waitGroup.Wait()

	if err := reloader.ReloadIfNecessary(); err != nil {
		log.Fatalf("failed to reload: %s", err)
	}
}

func writePidfile(pidfile string) {
	if _, err := os.Stat(pidfile); err != nil && !os.IsNotExist(err) {
		log.Fatalf("could not write pidfile %s: %v", pidfile, err)
	} else if err == nil {
		log.Fatalf("pidfile %s already exists", pidfile)
	}

	if file, err := os.Create(pidfile); err == nil {
		fmt.Fprintf(file, "%d\n", os.Getpid())
		defer file.Close()
	} else {
		log.Fatalf("could not write pidfile %s: %s", pidfile, err)
	}
}
