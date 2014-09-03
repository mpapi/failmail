package main

import (
	"fmt"
	"github.com/hut8labs/failmail/configure"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const VERSION = "0.1.0"

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
	sending := make(chan OutgoingMessage, 64)

	// Handle SIGINT and SIGTERM for cleaner shutdown.
	signals := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	go func() {
		for sig := range signals {
			log.Printf("caught signal %s", sig)
			done <- true
		}
	}()
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	auth, err := config.Auth()
	if err != nil {
		log.Fatalf("failed to parse auth credentials: %s", err)
	}

	tlsConfig, err := config.TLSConfig()

	// The listener talks SMTP to clients, and puts any messages they send onto
	// the `received` channel.
	socket, err := config.Socket()
	if err != nil {
		log.Fatalf("failed to create socket for listener: %s", err)
	}

	reloader := NewReloader(done)
	go reloader.HandleSignals()

	listener := &Listener{Logger: logger("listener"), Socket: socket, Auth: auth, TLSConfig: tlsConfig}
	go listener.Listen(received, reloader)

	// Figure out how to batch messages into separate summary emails. By
	// default, use the value of the --batch-header argument (falling back to
	// empty string, meaning all messages end up in the same summary email).
	batch := config.Batch()

	// Figure out how to group like messages within a summary. By default,
	// those with the same subject are considered the same.
	group := config.Group()

	if config.Pidfile != "" {
		writePidfile(config.Pidfile)
		defer os.Remove(config.Pidfile)
	}

	// A `MessageBuffer` collects incoming messages and decides how to batch
	// them up and when to relay them to an upstream SMTP server.
	buffer := NewMessageBuffer(config.WaitPeriod, config.MaxWait, batch, group, config.From)

	// A `RateCounter` watches the rate at which incoming messages are arriving
	// and can determine whether we've exceeded some threshold, for alerting.
	rateCounter := NewRateCounter(config.RateLimit, config.RateWindow)

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

	if config.Script != "" {
		runner, err := runScript(config.Script)
		if err != nil {
			log.Fatalf("failed to run script file %s: %s", config.Script, err)
		}
		go runner(done)
	}

	go ListenHTTP(config.BindHTTP, buffer)
	go run(buffer, rateCounter, config.RateCheck, received, sending, done, config.RelayAll)
	sendUpstream(sending, upstream, failedMaildir)

	reloader.ReloadIfNecessary()
}

func sendUpstream(sending <-chan OutgoingMessage, upstream Upstream, failedMaildir *Maildir) {
	for msg := range sending {
		if sendErr := upstream.Send(msg); sendErr != nil {
			log.Printf("couldn't send message: %s", sendErr)
			if saveErr := failedMaildir.Write(msg.Parts().Bytes); saveErr != nil {
				log.Printf("couldn't save message: %s", saveErr)
			}
		}
	}
	log.Printf("done sending")
}

func run(buffer *MessageBuffer, rateCounter *RateCounter, rateCheck time.Duration, received <-chan *ReceivedMessage, sending chan<- OutgoingMessage, done <-chan bool, relayAll bool) {

	tick := time.Tick(1 * time.Second)
	rateCheckTick := time.Tick(rateCheck)

	for {
		select {
		case <-tick:
			for _, summary := range buffer.Flush(false) {
				sending <- summary
			}
		case <-rateCheckTick:
			// Slide the window, and see if this interval trips the alert.
			exceeded, count := rateCounter.CheckAndAdvance()
			if exceeded {
				// TODO actually send an email here, eventually
				log.Printf("rate limit check exceeded: %d messages", count)
			}
		case msg := <-received:
			buffer.Add(msg)
			rateCounter.Add(1)
			if relayAll {
				sending <- msg
			}
		case <-done:
			log.Printf("cleaning up")
			for _, summary := range buffer.Flush(true) {
				sending <- summary
			}
			close(sending)
			break
		}
	}
}

func writePidfile(pidfile string) {
	if _, err := os.Stat(pidfile); !os.IsNotExist(err) {
		log.Fatalf("could not write pidfile %s: %s", pidfile, err)
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
