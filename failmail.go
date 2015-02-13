package main

import (
	"fmt"
	"github.com/hut8labs/failmail/configure"
	"log"
	"os"
	"sync"
)

//go:generate ./version.sh

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

func init() {
	log.SetPrefix(fmt.Sprintf("%d ", os.Getpid()))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
}

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

	signalListeners := make([]chan<- TerminationRequest, 0)
	waitGroup := new(sync.WaitGroup)

	reloadFd := uintptr(0)

	if config.Receiver {
		listener, err := config.MakeReceiver()
		if err != nil {
			log.Fatalf("failed to create listener: %s", err)
		}

		writer, err := config.MakeWriter()
		if err != nil {
			log.Fatalf("failed to create writer: %s", err)
		}

		// A channel for incoming messages. The listener sends on the channel, and
		// receives are added to a MessageBuffer in the channel consumer below.
		received := make(chan *StorageRequest, 64)

		done := make(chan TerminationRequest, 1)
		signalListeners = append(signalListeners, done)

		// Start a goroutine for receiving incoming meSsages.
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			reloadFd, err = listener.Listen(received, done, config.ShutdownTimeout)
			if err != nil {
				log.Printf("receiver failed to shut down cleanly: %s", err)
			} else {
				log.Printf("receiver: done")
			}
		}()

		// Start a goroutine for storing received messages.
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if err := writer.Run(received); err != nil {
				log.Printf("writer failed to shut down cleanly: %s", err)
			} else {
				log.Printf("writer: done")
			}
		}()
	}

	if config.Sender {
		// A `MessageBuffer` collects incoming messages and decides how to batch
		// them up and when to relay them to an upstream SMTP server.
		buffer, err := config.MakeSummarizer()
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

		// Start a goroutine for summarizing messages in the store.
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			buffer.Run(config.Poll, outgoing, done)
			log.Printf("summarizer: done")
		}()

		// Start a goroutine for sending summarized messages.
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

	// Handle signals for reloading/shutdown, then wait for the
	// message-handling goroutines to finish.
	shouldReload := HandleSignals(signalListeners)
	waitGroup.Wait()

	// Reload if necessary.
	if err := TryReload(shouldReload, reloadFd); err != nil {
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
