// Implementations for sending/relaying email messages, based around the
// `OutgoingMessage` interface.
package main

import (
	"io"
	"log"
	"net"
	"net/smtp"
)

// `Upstream` is the interface that wraps the method to send an
// `OutgoingMessage`.
type Upstream interface {
	Send(OutgoingMessage) error
}

// A `LiveUpstream` represents an upstream SMTP server that we can connect to
// for sending email messages.
type LiveUpstream struct {
	*log.Logger
	Addr string

	// Used for PLAIN auth if non-empty.
	User     string
	Password string
}

// Builds an Auth object, or nil if no authentication should be used to connect
// to this upstream server.
func (u *LiveUpstream) auth() smtp.Auth {
	if len(u.User) == 0 && len(u.Password) == 0 {
		return nil
	}
	host, _, _ := net.SplitHostPort(u.Addr)
	return smtp.PlainAuth("", u.User, u.Password, host)
}

func (u *LiveUpstream) Send(m OutgoingMessage) error {
	from := m.Sender()
	to := m.Recipients()
	u.Printf("sending message to %v", to)
	auth := u.auth()
	return smtp.SendMail(u.Addr, auth, from, to, m.Contents())
}

type DebugUpstream struct {
	Output io.Writer
}

func (u *DebugUpstream) Send(m OutgoingMessage) error {
	u.Output.Write(m.Contents())
	return nil
}

type MaildirUpstream struct {
	Maildir *Maildir
}

func (u *MaildirUpstream) Send(m OutgoingMessage) error {
	u.Maildir.Write(m.Contents())
	return nil
}

type MultiUpstream struct {
	upstreams []Upstream
}

func NewMultiUpstream(upstreams ...Upstream) Upstream {
	return &MultiUpstream{upstreams}
}

func (u *MultiUpstream) Send(m OutgoingMessage) error {
	for _, upstream := range u.upstreams {
		if err := upstream.Send(m); err != nil {
			return err
		}
	}
	return nil
}
