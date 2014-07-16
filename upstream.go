package main

import (
	"io"
	"log"
	"net"
	"net/smtp"
)

type Upstream interface {
	Send(OutgoingMessage) error
}

type LiveUpstream struct {
	*log.Logger
	Addr     string
	User     string
	Password string
}

func (u *LiveUpstream) auth() smtp.Auth {
	if len(u.User) == 0 && len(u.Password) == 0 {
		return nil
	}
	host, _, _ := net.SplitHostPort(u.Addr)
	return smtp.PlainAuth("", u.User, u.Password, host)
}

func (u *LiveUpstream) Send(m OutgoingMessage) error {
	parts := m.Parts()
	u.Printf("sending: %s", parts.Description)
	auth := u.auth()
	return smtp.SendMail(u.Addr, auth, parts.From, parts.To, parts.Bytes)
}

type DebugUpstream struct {
	Output io.Writer
}

func (u *DebugUpstream) Send(m OutgoingMessage) error {
	parts := m.Parts()
	u.Output.Write(parts.Bytes)
	return nil
}

type MaildirUpstream struct {
	Maildir *Maildir
}

func (u *MaildirUpstream) Send(m OutgoingMessage) error {
	parts := m.Parts()
	u.Maildir.Write(parts.Bytes)
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
