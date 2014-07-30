package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"
)

const VERSION = "0.1.0"

func main() {
	var (
		bindAddr     = flag.String("bind", "localhost:2525", "local bind address")
		relayAddr    = flag.String("relay", "localhost:25", "relay server address")
		waitPeriod   = flag.Duration("wait", 30*time.Second, "wait this long for more batchable messages")
		maxWait      = flag.Duration("max-wait", 5*time.Minute, "wait at most this long from first message to send summary")
		from         = flag.String("from", DefaultFromAddress("failmail"), "from address")
		failDir      = flag.String("fail-dir", "failed", "write failed sends to this maildir")
		allDir       = flag.String("all-dir", "", "write all sends to this maildir")
		rateLimit    = flag.Int("rate-limit", 0, "alert if this many emails are received within a window")
		rateCheck    = flag.Duration("rate-check", 1*time.Minute, "how often to check whether rate limit was exceeded")
		rateWindow   = flag.Int("rate-window", 5, "the size of the rate limit window, in check intervals")
		batchHeader  = flag.String("batch-header", "X-Failmail-Split", "the name of the header to use to separate messages into summary mails")
		batchReplace = flag.String("batch-subject-replace", "", "batch messages into summarizes whose subjects are the same after stripping out characters that match this regexp")
		batchMatch   = flag.String("batch-subject-match", "", "batch messages into summarizes whose subjects are the same after using only the characters that match this regexp")
		groupReplace = flag.String("group-subject-replace", "", "group messages within summarizes whose subjects are the same after stripping out characters that match this regexp")
		groupMatch   = flag.String("group-subject-match", "", "group messages within summarizes whose subjects are the same after using only the characters that match this regexp")
		bindHTTP     = flag.String("bind-http", "localhost:8025", "local bind address for the HTTP server")
		relayAll     = flag.Bool("relay-all", false, "relay all messages to the upstream server")

		relayUser     = flag.String("relay-user", "", "username for auth to relay server")
		relayPassword = flag.String("relay-password", "", "password for auth to relay server")

		relayCommand = flag.String("relay-command", "", "relay messages by running this command and passing the message to stdin")

		version = flag.Bool("version", false, "show the version number and exit")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Failmail %s\n\nUsage of %s:\n", VERSION, os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	if *version {
		fmt.Fprintf(os.Stderr, "Failmail %s\n", VERSION)
		return
	}

	// A channel for incoming messages. The listener sends on the channel, and
	// receives are added to a MessageBuffer in the channel consumer below.
	received := make(chan *ReceivedMessage, 64)

	// A channel for outgoing messages.
	sending := make(chan OutgoingMessage, 64)

	// TODO add a signal handler for clean shutdown with flush.
	done := make(chan bool, 0)

	// The listener talks SMTP to clients, and puts any messages they send onto
	// the `received` channel.
	listener := &Listener{Logger: logger("listener"), Addr: *bindAddr}
	go listener.Listen(received)

	// Figure out how to batch messages into separate summary emails. By
	// default, use the value of the --batch-header argument (falling back to
	// empty string, meaning all messages end up in the same summary email).
	batch := buildBatch(*batchMatch, *batchReplace, *batchHeader)

	// Figure out how to group like messages within a summary. By default,
	// those with the same subject are considered the same.
	group := buildGroup(*groupMatch, *groupReplace)

	// A `MessageBuffer` collects incoming messages and decides how to batch
	// them up and when to relay them to an upstream SMTP server.
	buffer := NewMessageBuffer(*waitPeriod, *maxWait, batch, group)

	// A `RateCounter` watches the rate at which incoming messages are arriving
	// and can determine whether we've exceeded some threshold, for alerting.
	rateCounter := NewRateCounter(*rateLimit, *rateWindow)

	// An upstream SMTP server is used to send the summarized messages flushed
	// from the MessageBuffer.
	upstream, err := buildUpstream(*relayAddr, *relayUser, *relayPassword, *allDir, *relayCommand)
	if err != nil {
		log.Fatalf("failed to create upstream: %s", err)
	}

	// Any messages we were unable to send upstream will be written to this
	// maildir.
	failedMaildir := &Maildir{Path: *failDir}
	if err := failedMaildir.Create(); err != nil {
		log.Fatalf("failed to create maildir for failed messages at %s: %s", *failDir, err)
	}

	go ListenHTTP(*bindHTTP, buffer)
	go sendUpstream(sending, upstream, failedMaildir)
	run(buffer, *from, rateCounter, *rateCheck, *rateWindow, received, sending, done, *relayAll)
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
}

func run(buffer *MessageBuffer, from string, rateCounter *RateCounter, rateCheck time.Duration, rateWindow int, received <-chan *ReceivedMessage, sending chan<- OutgoingMessage, done <-chan bool, relayAll bool) {

	tick := time.Tick(1 * time.Second)
	rateCheckTick := time.Tick(rateCheck)

	for {
		select {
		case <-tick:
			for _, summary := range buffer.Flush(from) {
				sending <- summary
			}
		case <-rateCheckTick:
			// Slide the window, and see if this interval trips the alert.
			exceeded, count := rateCounter.CheckAndAdvance()
			if exceeded {
				// TODO actually send an email here, eventually
				log.Printf("rate limit check exceeded: %d messages in the last %s", count, rateCheck*time.Duration(rateWindow))
			}
		case msg := <-received:
			buffer.Add(msg)
			rateCounter.Add(1)
			if relayAll {
				sending <- msg
			}
		}
	}
}

func buildBatch(batchMatch, batchReplace, batchHeader string) GroupBy {
	if batchMatch != "" {
		return MatchingSubject(batchMatch)
	} else if batchReplace != "" {
		return ReplacedSubject(batchReplace, "*")
	}
	return Header(batchHeader, "")
}

func buildGroup(groupMatch, groupReplace string) GroupBy {
	if groupMatch != "" {
		return MatchingSubject(groupMatch)
	} else if groupReplace != "" {
		return ReplacedSubject(groupReplace, "*")
	}
	return SameSubject()
}

func buildUpstream(relayAddr, relayUser, relayPassword, allDir, relayCommand string) (Upstream, error) {
	var upstream Upstream
	if relayAddr == "debug" {
		upstream = &DebugUpstream{os.Stdout}
	} else {
		upstream = &LiveUpstream{logger("upstream"), relayAddr, relayUser, relayPassword}
	}

	if allDir != "" {
		allMaildir := &Maildir{Path: allDir}
		if err := allMaildir.Create(); err != nil {
			return upstream, err
		}
		upstream = NewMultiUpstream(&MaildirUpstream{allMaildir}, upstream)
	}

	if relayCommand != "" {
		upstream = NewMultiUpstream(&ExecUpstream{relayCommand}, upstream)
	}
	return upstream, nil
}
