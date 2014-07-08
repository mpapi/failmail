package main

import (
	p "github.com/hut8labs/failmail/parse"
)

type Parser func(string) *p.Node

func SMTPParser() func(string) *p.Node {
	space := p.Regexp(`\s+`)
	name := p.Regexp(`[a-zA-z]([a-zA-Z0-9-]*[a-zA-Z0-9])?`)

	domain := p.Any()
	domain.Add(p.Separating(".", name, domain), name)

	snum := p.Regexp(`([0-9]|[0-9][0-9|1[0-9][0-9]|2[0-4][0-9]|25[0-5])`)
	addr := p.Separating(".", snum, snum, snum, snum)
	addressLiteral := p.Surrounding("[", "]", addr)

	str := p.Regexp(`(\\.|[^ <>\(\)\[\]\\.,;:@"\r\n])+`)

	dotString := p.Any()
	dotString.Add(p.Series(str, p.Literal("."), dotString), str)

	quotedString := p.Regexp(`"(\\.|[^ \r\n"\\])+"`)

	localPart := p.Any(dotString, quotedString)
	mailbox := p.Separating("@", localPart, p.Any(addressLiteral, domain))

	path := p.Surrounding("<", ">", mailbox)
	reversePath := p.Any(path, p.Literal("<>"))

	Command := func(str string) p.Parser {
		return p.Label("command", p.ILiteral(str))
	}

	Line := func(parsers ...p.Parser) p.Parser {
		s := p.Series(parsers...)
		s.Add(p.Literal("\r\n"))
		return s
	}

	// RFC 821
	helo := Line(Command("HELO"), space, p.Label("domain", domain))
	mail := Line(Command("MAIL"), space, p.ILiteral("FROM:"), p.Label("path", reversePath))
	rcpt := Line(Command("RCPT"), space, p.ILiteral("TO:"), p.Label("path", path))
	data := Line(Command("DATA"))
	rset := Line(Command("RSET"))
	noop := Line(Command("NOOP"))
	quit := Line(Command("QUIT"))

	// RFC 2821
	vrfy := Line(Command("VRFY"), space, p.Label("text", str))
	ehlo := Line(Command("EHLO"), space, p.Label("domain", p.Any(addressLiteral, domain)))

	smtp := p.Any(helo, mail, rcpt, data, rset, noop, quit, ehlo, vrfy)

	return func(str string) *p.Node {
		_, node := smtp.Parse(str)
		return node
	}
}
