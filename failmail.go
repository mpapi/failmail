package main

import (
	"crypto/tls"
	"fmt"
	"github.com/hut8labs/failmail/configure"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const VERSION = "0.1.0"

type Config struct {
	BindAddr     string        `help:"local bind address"`
	RelayAddr    string        `help:"relay server address"`
	WaitPeriod   time.Duration `help:"wait this long for more batchable messages"`
	MaxWait      time.Duration `help:"wait at most this long from first message to send summary"`
	From         string        `help:"from address"`
	FailDir      string        `help:"write failed sends to this maildir"`
	AllDir       string        `help:"write all sends to this maildir"`
	RateLimit    int           `help:"alert if this many emails are received within a window"`
	RateCheck    time.Duration `help:"how often to check whether rate limit was exceeded"`
	RateWindow   int           `help:"the size of the rate limit window, in check intervals"`
	BatchHeader  string        `help:"the name of the header to use to separate messages into summary mails"`
	BatchReplace string        `help:"batch messages into summaries whose subjects are the same after stripping out characters that match this regexp"`
	BatchMatch   string        `help:"batch messages into summaries whose subjects are the same after using only the characters that match this regexp"`
	GroupReplace string        `help:"group messages within summaries whose subjects are the same after stripping out characters that match this regexp"`
	GroupMatch   string        `help:"group messages within summaries whose subjects are the same after using only the characters that match this regexp"`
	BindHTTP     string        `help:"local bind address for the HTTP server"`
	RelayAll     bool          `help:"relay all messages to the upstream server"`

	RelayUser     string `help:"username for auth to relay server"`
	RelayPassword string `help:"password for auth to relay server"`
	Credentials   string `help:"username:password for authenticating to failmail"`
	TlsCert       string `help:"PEM certificate file for TLS"`
	TlsKey        string `help:"PEM key file for TLS"`

	RelayCommand string `help:"relay messages by running this command and passing the message to stdin"`

	Script  string `help:"SMTP session script to run"`
	Version bool   `help:"show the version number and exit"`
}

func Defaults() *Config {
	return &Config{
		BindAddr:    "localhost:2525",
		RelayAddr:   "localhost:25",
		WaitPeriod:  30 * time.Second,
		MaxWait:     5 * time.Minute,
		From:        DefaultFromAddress("failmail"),
		FailDir:     "failed",
		RateCheck:   1 * time.Minute,
		RateWindow:  5,
		BatchHeader: "X-Failmail-Split",
		BindHTTP:    "localhost:8025",
	}
}

func main() {
	config := Defaults()

	err := configure.Parse(config, fmt.Sprintf("failmail %s", VERSION))
	if err != nil {
		log.Fatalf("Failed to read configuration: %s", err)
	}

	if config.Version {
		fmt.Fprintf(os.Stderr, "Failmail %s\n", VERSION)
		return
	}

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

	auth, err := buildAuth(config.Credentials)
	if err != nil {
		log.Fatalf("failed to parse auth credentials: %s", err)
	}

	tlsConfig, err := buildTLSConfig(config.TlsCert, config.TlsKey)

	// The listener talks SMTP to clients, and puts any messages they send onto
	// the `received` channel.
	listener := &Listener{Logger: logger("listener"), Addr: config.BindAddr, Auth: auth, TLSConfig: tlsConfig}
	go listener.Listen(received)

	// Figure out how to batch messages into separate summary emails. By
	// default, use the value of the --batch-header argument (falling back to
	// empty string, meaning all messages end up in the same summary email).
	batch := buildBatch(config.BatchMatch, config.BatchReplace, config.BatchHeader)

	// Figure out how to group like messages within a summary. By default,
	// those with the same subject are considered the same.
	group := buildGroup(config.GroupMatch, config.GroupReplace)

	// A `MessageBuffer` collects incoming messages and decides how to batch
	// them up and when to relay them to an upstream SMTP server.
	buffer := NewMessageBuffer(config.WaitPeriod, config.MaxWait, batch, group, config.From)

	// A `RateCounter` watches the rate at which incoming messages are arriving
	// and can determine whether we've exceeded some threshold, for alerting.
	rateCounter := NewRateCounter(config.RateLimit, config.RateWindow)

	// An upstream SMTP server is used to send the summarized messages flushed
	// from the MessageBuffer.
	upstream, err := buildUpstream(config.RelayAddr, config.RelayUser, config.RelayPassword, config.AllDir, config.RelayCommand)
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

func buildAuth(credentials string) (Auth, error) {
	if credentials == "" {
		return nil, nil
	}

	parts := strings.SplitN(credentials, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("credentials must be in username:password format")
	}

	return &SingleUserPlainAuth{Username: parts[0], Password: parts[1]}, nil
}

func buildTLSConfig(certFile string, keyFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}
