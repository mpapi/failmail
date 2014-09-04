package main

import (
	"crypto/tls"
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	BindAddr     string        `help:"local bind address"`
	RelayAddr    string        `help:"relay server address"`
	SocketFd     int           `help:"file descriptor of socket to listen on"`
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
	Pidfile      string        `help:"write a pidfile to this path"`

	ShutdownTimeout time.Duration `help:"wait this long for open connections to finish when shutting down or reloading"`

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
		BindAddr:        "localhost:2525",
		RelayAddr:       "localhost:25",
		WaitPeriod:      30 * time.Second,
		MaxWait:         5 * time.Minute,
		From:            DefaultFromAddress("failmail"),
		FailDir:         "failed",
		RateCheck:       1 * time.Minute,
		RateWindow:      5,
		BatchHeader:     "X-Failmail-Split",
		BindHTTP:        "localhost:8025",
		ShutdownTimeout: 5 * time.Second,
	}
}

func (c *Config) Auth() (Auth, error) {
	if c.Credentials == "" {
		return nil, nil
	}

	parts := strings.SplitN(c.Credentials, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("credentials must be in username:password format")
	}

	return &SingleUserPlainAuth{Username: parts[0], Password: parts[1]}, nil
}

func (c *Config) Batch() GroupBy {
	if c.BatchMatch != "" {
		return MatchingSubject(c.BatchMatch)
	} else if c.BatchReplace != "" {
		return ReplacedSubject(c.BatchReplace, "*")
	}
	return Header(c.BatchHeader, "")
}

func (c *Config) Group() GroupBy {
	if c.GroupMatch != "" {
		return MatchingSubject(c.GroupMatch)
	} else if c.GroupReplace != "" {
		return ReplacedSubject(c.GroupReplace, "*")
	}
	return SameSubject()
}

func (c *Config) Upstream() (Upstream, error) {
	var upstream Upstream
	if c.RelayAddr == "debug" {
		upstream = &DebugUpstream{os.Stdout}
	} else {
		upstream = &LiveUpstream{logger("upstream"), c.RelayAddr, c.RelayUser, c.RelayPassword}
	}

	if c.AllDir != "" {
		allMaildir := &Maildir{Path: c.AllDir}
		if err := allMaildir.Create(); err != nil {
			return upstream, err
		}
		upstream = NewMultiUpstream(&MaildirUpstream{allMaildir}, upstream)
	}

	if c.RelayCommand != "" {
		upstream = NewMultiUpstream(&ExecUpstream{c.RelayCommand}, upstream)
	}
	return upstream, nil
}

func (c *Config) TLSConfig() (*tls.Config, error) {
	if c.TlsCert == "" || c.TlsKey == "" {
		return nil, nil
	}

	cert, err := tls.LoadX509KeyPair(c.TlsCert, c.TlsKey)
	if err != nil {
		return nil, err
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}}, nil
}

func (c *Config) Socket() (ServerSocket, error) {
	if c.SocketFd > 0 {
		return NewFileServerSocket(uintptr(c.SocketFd))
	}
	return NewTCPServerSocket(c.BindAddr)
}
