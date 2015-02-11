package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/mail"
	"regexp"
	"strings"
	"text/template"
	"time"
)

// `OutgoingMessage` is the interface for any message that can be sent.
// `Sender()` and `Recipients()` are used to specify the envelope From/To, and
// `Contents()` contains the SMTP `DATA` payload (usually an RFC822 message).
type OutgoingMessage interface {
	Sender() string
	Recipients() []string
	Contents() []byte
}

// A simple `OutgoingMessage` implementation, where the various parts are known
// ahead of time.
type message struct {
	From string
	To   []string
	Data []byte
}

func (m *message) Sender() string {
	return m.From
}

func (m *message) Recipients() []string {
	return m.To
}

func (m *message) Contents() []byte {
	return m.Data
}

// A message received from an SMTP client. These get compacted into
// `UniqueMessage`s, many which are then periodically sent via an upstream
// server in a `SummaryMessage`.
type ReceivedMessage struct {
	*message
	Parsed *mail.Message
}

func (r *ReceivedMessage) ReadBody() string {
	if r.Parsed == nil {
		return "[no message body]"
	} else if body, err := ioutil.ReadAll(r.Parsed.Body); err != nil {
		return "[unreadable message body]"
	} else {
		return string(body)
	}
}

func (r *ReceivedMessage) DisplayDate(def string) string {
	if d, err := r.Parsed.Header.Date(); err != nil {
		return def
	} else {
		return d.Format(time.RFC1123Z)
	}
}

// A `UniqueMessage` is the result of compacting similar `ReceivedMessage`s.
type UniqueMessage struct {
	Start    time.Time
	End      time.Time
	Body     string
	Subject  string
	Template string
	Count    int
}

// `Compact` returns a `UniqueMessage` for each distinct key among the received
// messages, using the regular expression `sanitize` to create a representative
// template body for the `UniqueMessage`.
func Compact(group GroupBy, stored []*StoredMessage) []*UniqueMessage {
	uniques := make(map[string]*UniqueMessage)
	result := make([]*UniqueMessage, 0)
	for _, msg := range stored {
		key, _ := group(msg.ReceivedMessage)

		if _, ok := uniques[key]; !ok {
			unique := &UniqueMessage{Template: key}
			uniques[key] = unique
			result = append(result, unique)
		}
		unique := uniques[key]

		if date, err := msg.Parsed.Header.Date(); err == nil {
			if unique.Start.IsZero() || date.Before(unique.Start) {
				unique.Start = date
			}
			if unique.End.IsZero() || date.After(unique.End) {
				unique.End = date
			}
		}
		unique.Body = msg.ReadBody()
		unique.Subject = msg.Parsed.Header.Get("subject")
		unique.Count += 1
	}
	return result
}

// A `SummaryMessage` is the result of rolling together several
// `UniqueMessage`s.
type SummaryMessage struct {
	From           string
	To             []string
	Subject        string
	Date           time.Time
	StoredMessages []*StoredMessage
	UniqueMessages []*UniqueMessage
}

func (s *SummaryMessage) Sender() string {
	return s.From
}

func (s *SummaryMessage) Recipients() []string {
	return s.To
}

func (s *SummaryMessage) Headers() string {
	buf := new(bytes.Buffer)
	s.writeHeaders(buf)
	return buf.String()
}

func (s *SummaryMessage) writeHeaders(buf *bytes.Buffer) {
	fmt.Fprintf(buf, "From: %s\r\n", s.From)
	fmt.Fprintf(buf, "To: %s\r\n", strings.Join(s.To, ", "))
	fmt.Fprintf(buf, "Subject: %s\r\n", s.Subject)
	fmt.Fprintf(buf, "Date: %s\r\n", s.Date.Format(time.RFC822))
	fmt.Fprintf(buf, "\r\n")
}

type SummaryStats struct {
	TotalMessages    int
	FirstMessageTime time.Time
	LastMessageTime  time.Time
}

func (s *SummaryMessage) Stats() *SummaryStats {
	var total int
	var firstMessageTime time.Time
	var lastMessageTime time.Time

	for _, unique := range s.UniqueMessages {
		total += unique.Count
		if firstMessageTime.IsZero() || unique.Start.Before(firstMessageTime) {
			firstMessageTime = unique.Start
		}
		if lastMessageTime.IsZero() || unique.End.After(lastMessageTime) {
			lastMessageTime = unique.End
		}
	}
	return &SummaryStats{total, firstMessageTime, lastMessageTime}
}

func (s *SummaryMessage) Contents() []byte {
	buf := new(bytes.Buffer)
	s.writeHeaders(buf)

	stats := s.Stats()

	body := new(bytes.Buffer)
	for i, unique := range s.UniqueMessages {
		fmt.Fprintf(body, "\r\n- Message group %d of %d: %d instances\r\n", i+1, len(s.UniqueMessages), unique.Count)
		fmt.Fprintf(body, "  From %s to %s\r\n\r\n", unique.Start.Format(time.RFC1123Z), unique.End.Format(time.RFC1123Z))
		fmt.Fprintf(body, "Subject: %#v\r\nBody:\r\n%s\r\n", unique.Subject, unique.Body)

	}

	fmt.Fprintf(buf, "--- Failmail ---\r\n")
	fmt.Fprintf(buf, "Total messages: %d\r\nUnique messages: %d\r\n", stats.TotalMessages, len(s.UniqueMessages))
	fmt.Fprintf(buf, "Oldest message: %s\r\nNewest message: %s\r\n", stats.FirstMessageTime.Format(time.RFC1123Z), stats.LastMessageTime.Format(time.RFC1123Z))
	fmt.Fprintf(buf, "%s", body.Bytes())
	return buf.Bytes()
}

func Summarize(group GroupBy, from string, to string, stored []*StoredMessage) *SummaryMessage {
	uniques := Compact(group, stored)
	result := &SummaryMessage{}

	result.From = from
	result.To = []string{to}
	result.Date = nowGetter()

	instances := Plural(len(stored), "instance", "instances")
	if len(uniques) == 1 {
		result.Subject = fmt.Sprintf("[failmail] %s: %s", instances, uniques[0].Subject)
	} else {
		messages := Plural(len(uniques), "message", "messages")
		result.Subject = fmt.Sprintf("[failmail] %s of %s", instances, messages)
	}

	result.StoredMessages = stored
	result.UniqueMessages = uniques
	return result
}

type MessageBuffer struct {
	SoftLimit time.Duration
	HardLimit time.Duration
	Batch     GroupBy // determines how messages are split into summary emails
	Group     GroupBy // determines how messages are grouped within summary emails
	From      string
	Store     MessageStore
	Renderer  SummaryRenderer
	lastFlush time.Time
	*batches
}

type batches struct {
	first    map[RecipientKey]time.Time
	last     map[RecipientKey]time.Time
	messages map[RecipientKey][]*StoredMessage
}

func NewBatches() *batches {
	return &batches{
		make(map[RecipientKey]time.Time, 0),
		make(map[RecipientKey]time.Time, 0),
		make(map[RecipientKey][]*StoredMessage, 0),
	}
}

func (b *batches) Add(key RecipientKey, s *StoredMessage) {
	if _, ok := b.first[key]; !ok {
		b.first[key] = s.Received
		b.messages[key] = make([]*StoredMessage, 0)
	}
	b.last[key] = s.Received
	b.messages[key] = append(b.messages[key], s)
}

func (b *batches) Remove(key RecipientKey) {
	delete(b.messages, key)
	delete(b.first, key)
	delete(b.last, key)
}

func (b *MessageBuffer) NeedsFlush(now time.Time, key RecipientKey) bool {
	return !(now.Sub(b.first[key]) < b.HardLimit && now.Sub(b.last[key]) < b.SoftLimit)
}

func (b *MessageBuffer) Flush(outgoing chan<- *SendRequest, force bool) {
	now := nowGetter()

	// Get messages newer than the last flush.
	stored, _ := b.Store.MessagesNewerThan(b.lastFlush)
	for _, s := range stored {
		key, _ := b.Batch(s.ReceivedMessage)
		for _, to := range s.Recipients() {
			recipKey := RecipientKey{key, NormalizeAddress(to)}
			b.Add(recipKey, s)
		}
	}

	toRemove := make(map[MessageId]bool, 0)
	toKeep := make(map[MessageId]bool, 0)

	// Summarize message groups that are due to be sent.
	for key, msgs := range b.messages {
		if force || b.NeedsFlush(now, key) {
			summary := Summarize(b.Group, b.From, key.Recipient, msgs)

			sendErrors := make(chan error, 0)
			outgoing <- &SendRequest{b.Renderer.Render(summary), sendErrors}
			if err := <-sendErrors; err != nil {
				// If we failed to send, make sure we keep the messages.
				for _, msg := range msgs {
					toKeep[msg.Id] = true
				}
			} else {
				// If we sent successfully, get rid of the messages.
				for _, msg := range msgs {
					toRemove[msg.Id] = true
				}
				b.Remove(key)
			}

		}
	}

	// Remove any that were summarized.
	for id, _ := range toRemove {
		if err := b.Store.Remove(id); err != nil {
			log.Printf("failed to remove message from store: %s", err)
		}
	}

	b.lastFlush = now
}

func NormalizeAddress(email string) string {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return email
	}
	return strings.ToLower(addr.Address)
}

func (b *MessageBuffer) Stats() *BufferStats {
	uniqueMessages := 0
	allMessages := 0
	now := nowGetter()
	var lastReceived time.Time
	for key, msgs := range b.messages {
		if !b.NeedsFlush(now, key) {
			allMessages += len(msgs)
		}
		uniqueMessages += 1
		if lastReceived.Before(b.last[key]) {
			lastReceived = b.last[key]
		}
	}
	return &BufferStats{uniqueMessages, allMessages, lastReceived}
}

type RecipientKey struct {
	Key       string
	Recipient string
}

type BufferStats struct {
	ActiveBatches  int
	ActiveMessages int
	LastReceived   time.Time
}

func Plural(count int, singular string, plural string) string {
	var word string
	if count == 1 {
		word = singular
	} else {
		word = plural
	}
	return fmt.Sprintf("%d %s", count, word)
}

func DefaultFromAddress(name string) string {
	host, err := hostGetter()
	if err != nil {
		host = "localhost"
	}
	return fmt.Sprintf("%s@%s", name, host)
}

// TODO write full-text HTML and keep them for n days

type GroupBy func(*ReceivedMessage) (string, error)

func GroupByExpr(name string, expr string) GroupBy {
	funcMap := make(map[string]interface{})
	funcMap["match"] = func(pat string, text string) (string, error) {
		re, err := regexp.Compile(pat)
		return re.FindString(text), err
	}
	funcMap["replace"] = func(pat string, text string, sub string) (string, error) {
		re, err := regexp.Compile(pat)
		return re.ReplaceAllString(text, sub), err
	}

	tmpl := template.Must(template.New(name).Funcs(funcMap).Parse(expr))

	return func(r *ReceivedMessage) (string, error) {
		buf := new(bytes.Buffer)
		err := tmpl.Execute(buf, r.Parsed)
		return buf.String(), err
	}
}

func (b *MessageBuffer) Run(outgoing chan<- *SendRequest, done <-chan TerminationRequest) {
	tick := time.Tick(5 * time.Second)
	for {
		select {
		case <-tick:
			b.Flush(outgoing, false)
		case req := <-done:
			if req == GracefulShutdown {
				log.Printf("cleaning up")
				b.Flush(outgoing, true)
				close(outgoing)
				return
			}
		}
	}
}
