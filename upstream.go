package main

import (
	"io"
	"log"
	"net/smtp"
)

type Upstream interface {
	Send(OutgoingMessage) error
}

type LiveUpstream struct {
	*log.Logger
	Addr string
}

func (u *LiveUpstream) Send(m OutgoingMessage) error {
	parts := m.Parts()
	u.Printf("sending: %s", parts.Description)
	return smtp.SendMail(u.Addr, nil, parts.From, parts.To, parts.Bytes)
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
