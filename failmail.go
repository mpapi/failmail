package main

import (
	"fmt"
	"github.com/hut8labs/failmail/configure"
	"log"
	"os"
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

	// A channel for incoming messages. The listener sends on the channel, and
	// receives are added to a MessageBuffer in the channel consumer below.
	received := make(chan *ReceivedMessage, 64)

	// A channel for outgoing messages.
	outgoing := make(chan OutgoingMessage, 64)

	// Handle signals for reloading/shutdown.
	done := make(chan TerminationRequest, 1)
	go HandleSignals(done)

	listener, err := config.Listener()
	if err != nil {
		log.Fatalf("failed to create listener: %s", err)
	}

	reloader := NewReloader()
	go listener.Listen(received, reloader, config.ShutdownTimeout)

	if config.Pidfile != "" {
		writePidfile(config.Pidfile)
		defer os.Remove(config.Pidfile)
	}

	store, err := config.Store()
	if err != nil {
		log.Fatalf("failed to create message store: %s", err)
	}

	// A `MessageBuffer` collects incoming messages and decides how to batch
	// them up and when to relay them to an upstream SMTP server.
	buffer := &MessageBuffer{
		SoftLimit: config.WaitPeriod,
		HardLimit: config.MaxWait,
		Batch:     config.Batch(),
		Group:     config.Group(),
		From:      config.From,
		Store:     store,
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

	relay := &MessageRelay{
		Renderer: config.SummaryRenderer(),
		Buffer:   buffer,
		Reloader: reloader,
	}
	go relay.Run(received, done, outgoing)

	sender := &Sender{upstream, failedMaildir}
	sender.Run(outgoing)

	if err := reloader.ReloadIfNecessary(); err != nil {
		log.Fatalf("failed to reload: %s", err)
	}
}

func sendUpstream(outgoing <-chan OutgoingMessage, upstream Upstream, failedMaildir *Maildir) {
	for msg := range outgoing {
		if sendErr := upstream.Send(msg); sendErr != nil {
			log.Printf("couldn't send message: %s", sendErr)
			if _, saveErr := failedMaildir.Write([]byte(msg.Contents())); saveErr != nil {
				log.Printf("couldn't save message: %s", saveErr)
			}
		}
	}
	log.Printf("done sending")
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
