package main

import (
	"crypto/tls"
	"fmt"
	"os"
	"strings"
	"text/template"
	"time"
)

type Config struct {
	BindAddr   string        `help:"local bind address"`
	RelayAddr  string        `help:"relay server address"`
	SocketFd   int           `help:"file descriptor of socket to listen on"`
	WaitPeriod time.Duration `help:"wait this long for more batchable messages"`
	MaxWait    time.Duration `help:"wait at most this long from first message to send summary"`
	From       string        `help:"from address"`
	FailDir    string        `help:"write failed sends to this maildir"`
	AllDir     string        `help:"write all sends to this maildir"`
	BatchExpr  string        `help:"an expression used to determine how messages are batched into summary emails"`
	GroupExpr  string        `help:"an expression used to determine how messages are grouped within summary emails"`
	BindHTTP   string        `help:"local bind address for the HTTP server"`
	RelayAll   bool          `help:"relay all messages to the upstream server"`
	Pidfile    string        `help:"write a pidfile to this path"`
	Template   string        `help:"path to a summary message template file"`

	ShutdownTimeout time.Duration `help:"wait this long for open connections to finish when shutting down or reloading"`

	RelayUser     string `help:"username for auth to relay server"`
	RelayPassword string `help:"password for auth to relay server"`
	Credentials   string `help:"username:password for authenticating to failmail"`
	TlsCert       string `help:"PEM certificate file for TLS"`
	TlsKey        string `help:"PEM key file for TLS"`

	RelayCommand string `help:"relay messages by running this command and passing the message to stdin"`

	MemoryStore  bool   `help:"store messages in memory instead of an on-disk maildir"`
	MessageStore string `help:"use this directory as a maildir for holding received messages"`

	Version bool `help:"show the version number and exit"`
}

func Defaults() *Config {
	return &Config{
		BindAddr:        "localhost:2525",
		RelayAddr:       "localhost:25",
		WaitPeriod:      30 * time.Second,
		MaxWait:         5 * time.Minute,
		From:            DefaultFromAddress("failmail"),
		FailDir:         "failed",
		BatchExpr:       `{{.Header.Get "X-Failmail-Split"}}`,
		GroupExpr:       `{{.Header.Get "Subject"}}`,
		BindHTTP:        "localhost:8025",
		ShutdownTimeout: 5 * time.Second,
		MessageStore:    "incoming",
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
	return GroupByExpr("batch", c.BatchExpr)
}

func (c *Config) Group() GroupBy {
	return GroupByExpr("group", c.GroupExpr)
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

func (c *Config) SummaryRenderer() SummaryRenderer {
	if c.Template != "" {
		tmpl := template.Must(template.New(c.Template).Funcs(SUMMARY_TEMPLATE_FUNCS).ParseFiles(c.Template))
		return &TemplateRenderer{tmpl}
	}
	return &NoRenderer{}
}

func (c *Config) Store() (MessageStore, error) {
	switch {
	case c.MemoryStore:
		return NewMemoryStore(), nil
	case c.MessageStore == "":
		return nil, fmt.Errorf("must have either a memory store or a disk-backed store")
	default:
		maildir := &Maildir{Path: c.MessageStore}
		err := maildir.Create()
		if err != nil {
			return nil, err
		}
		return NewDiskStore(maildir)
	}
}
