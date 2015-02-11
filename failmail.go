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

	store, err := config.Store()
	if err != nil {
		log.Fatalf("failed to create message store: %s", err)
	}

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

		writer := &MessageWriter{store}
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
		buffer := &MessageBuffer{
			SoftLimit: config.WaitPeriod,
			HardLimit: config.MaxWait,
			Batch:     config.Batch(),
			Group:     config.Group(),
			From:      config.From,
			Store:     store,
			Renderer:  config.SummaryRenderer(),
			batches:   NewBatches(),
		}

		// An upstream SMTP server is used to send the summarized messages flushed
		// from the MessageBuffer.
		upstream, err := config.Upstream()
		if err != nil {
			log.Fatalf("failed to create upstream: %s", err)
		}

		// Any messages we were unable to send upstream will be written to this
		// maildir.
		failedMaildir := &Maildir{Path: config.FailDir}
		if err := failedMaildir.Create(); err != nil {
			log.Fatalf("failed to create maildir for failed messages at %s: %s", config.FailDir, err)
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

		sender := &Sender{upstream, failedMaildir}
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
