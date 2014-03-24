package main

import (
	"io"
	"log"
	"net/smtp"
)

type Upstream interface {
	Send(*SummaryMessage) error
}

type LiveUpstream struct {
	*log.Logger
	Addr string
}

func (u *LiveUpstream) Send(s *SummaryMessage) error {
	u.Printf("sending summary: %s", s.Subject)
	return smtp.SendMail(u.Addr, nil, s.From, s.To, s.Bytes())
}

type DebugUpstream struct {
	Output io.Writer
}

func (u *DebugUpstream) Send(s *SummaryMessage) error {
	u.Output.Write(s.Bytes())
	return nil
}

type MaildirUpstream struct {
	Maildir *Maildir
}

func (u *MaildirUpstream) Send(s *SummaryMessage) error {
	u.Maildir.Write(s.Bytes())
	return nil
}

type MultiUpstream struct {
	upstreams []Upstream
}

func NewMultiUpstream(upstreams ...Upstream) Upstream {
	return &MultiUpstream{upstreams}
}

func (u *MultiUpstream) Send(s *SummaryMessage) error {
	for _, upstream := range u.upstreams {
		if err := upstream.Send(s); err != nil {
			return err
		}
	}
	return nil
}
