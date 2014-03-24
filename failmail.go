package main

import (
	"flag"
	"log"
	"os"
	"time"
)

func main() {
	var (
		bindAddr   = flag.String("bind", "localhost:2525", "local bind address")
		relayAddr  = flag.String("relay", "localhost:25", "relay server address")
		waitPeriod = flag.Duration("wait", 30*time.Second, "wait this long for more batchable messages")
		maxWait    = flag.Duration("max-wait", 5*time.Minute, "wait at most this long from first message to send summary")
		from       = flag.String("from", DefaultFromAddress("failmail"), "from address")
		failDir    = flag.String("fail-dir", "failed", "write failed sends to this maildir")
		allDir     = flag.String("all-dir", "", "write all sends to this maildir")
	)
	flag.Parse()

	// A channel for incoming messages. The listener sends on the channel, and
	// receives are added to a MessageBuffer in the channel consumer below.
	received := make(chan *ReceivedMessage, 64)

	// The listener talks SMTP to clients, and puts any messages they send onto
	// the `received` channel.
	listener := &Listener{logger("listener"), *bindAddr}
	go listener.Listen(received)

	// A `MessageBuffer` collects incoming messages and decides how to batch
	// them up and when to relay them to an upstream SMTP server.
	buffer := NewMessageBuffer(*waitPeriod, *maxWait, Header("X-Failmail-Split", ""), SameSubject())

	// An upstream SMTP server is used to send the summarized messages flushed
	// from the MessageBuffer.
	var upstream Upstream
	if *relayAddr == "debug" {
		upstream = &DebugUpstream{os.Stdout}
	} else {
		upstream = &LiveUpstream{logger("upstream"), *relayAddr}
	}

	if *allDir != "" {
		allMaildir := &Maildir{Path: *allDir}
		if err := allMaildir.Create(); err != nil {
			log.Fatalf("failed to create maildir for all messages at %s: %s", *allDir, err)
		}
		upstream = NewMultiUpstream(&MaildirUpstream{allMaildir}, upstream)
	}

	// Any messages we were unable to send upstream will be written to this
	// maildir.
	failedMaildir := &Maildir{Path: *failDir}
	if err := failedMaildir.Create(); err != nil {
		log.Fatalf("failed to create maildir for failed messages at %s: %s", *failDir, err)
	}

	tick := time.Tick(1 * time.Second)
	for {
		select {
		case <-tick:
			for _, summary := range buffer.Flush(*from) {
				if sendErr := upstream.Send(summary); sendErr != nil {
					log.Printf("couldn't send message: %s", sendErr)
					if saveErr := failedMaildir.Write(summary.Bytes()); saveErr != nil {
						log.Printf("couldn't save message: %s", saveErr)
					}
				}
			}
		case msg := <-received:
			buffer.Add(msg)
		}
	}
}
