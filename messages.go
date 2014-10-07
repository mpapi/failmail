package main

import (
	"bytes"
	"container/ring"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/mail"
	"os"
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
func Compact(group GroupBy, received []*ReceivedMessage) []*UniqueMessage {
	uniques := make(map[string]*UniqueMessage)
	result := make([]*UniqueMessage, 0)
	for _, msg := range received {
		key, _ := group(msg)

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
	From             string
	To               []string
	Subject          string
	Date             time.Time
	ReceivedMessages []*ReceivedMessage
	UniqueMessages   []*UniqueMessage
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

func Summarize(group GroupBy, from string, to string, received []*ReceivedMessage) *SummaryMessage {
	uniques := Compact(group, received)
	result := &SummaryMessage{}

	result.From = from
	result.To = []string{to}
	result.Date = nowGetter()

	instances := Plural(len(received), "instance", "instances")
	if len(uniques) == 1 {
		result.Subject = fmt.Sprintf("[failmail] %s: %s", instances, uniques[0].Subject)
	} else {
		messages := Plural(len(uniques), "message", "messages")
		result.Subject = fmt.Sprintf("[failmail] %s of %s", instances, messages)
	}

	result.ReceivedMessages = received
	result.UniqueMessages = uniques
	return result
}

// `MessageStore` is the interface that provides storage and limited retrieval
// of messages for `MessageBuffer`.
type MessageStore interface {
	// Adds a message to the store, with the time it was received.
	Add(time.Time, RecipientKey, *ReceivedMessage)

	// Computes whether the receive time a message (given its key) was within a
	// certain duration of a time. (The first duration is for the time since
	// the message was first seen, and the second is for the time it was most
	// recently seen.)
	InRange(time.Time, RecipientKey, time.Duration, time.Duration) bool

	// Calls a function on each message in the store, removing it from the
	// store if the function returns true.
	Iterate(func(RecipientKey, []*ReceivedMessage, time.Time, time.Time) bool)
}

// `MessageMetadata` holds data that isn't part of the RFC822 message: the
// envelope, the time it was received, and the key used to determine the
// summary it gets rolled into.
type MessageMetadata struct {
	Received    time.Time
	Key         RecipientKey
	MessageFrom string
	MessageTo   []string
}

type messageTimes struct {
	first map[RecipientKey]time.Time
	last  map[RecipientKey]time.Time
}

func (t *messageTimes) InRange(now time.Time, key RecipientKey, softLimit time.Duration, hardLimit time.Duration) bool {
	return now.Sub(t.first[key]) < hardLimit && now.Sub(t.last[key]) < softLimit
}

// `DiskStore` is an implementation of `MessageStore` that uses a maildir on
// disk to hold messages. Currently, message metadata is stored in JSON files
// alongside the messages in the maildir.
type DiskStore struct {
	Maildir *Maildir

	messages map[RecipientKey][]string
	*messageTimes
}

// `NewDiskStore` creates a `DiskStore`, using `maildir` to back it. Any
// messages already in `maildir` are used to initialize the `DiskStore`
// effectively restoring its state e.g. after a crash.
func NewDiskStore(maildir *Maildir) (*DiskStore, error) {
	store := &DiskStore{
		maildir,
		make(map[RecipientKey][]string),
		&messageTimes{
			make(map[RecipientKey]time.Time),
			make(map[RecipientKey]time.Time),
		},
	}

	names, _ := maildir.List()
	for _, name := range names {
		if strings.HasPrefix(name, ".") {
			continue
		}

		// Get the metadata for the message and apply it to the store as though
		// it had been received at the time specified by the metadata.
		md, err := store.readMetadata(name)
		if err != nil {
			return store, err
		}
		store.restore(md, name)
	}
	return store, nil
}

func (s *DiskStore) restore(md *MessageMetadata, name string) {
	if first, ok := s.first[md.Key]; !ok || md.Received.Before(first) {
		s.first[md.Key] = md.Received
	}
	if last, ok := s.last[md.Key]; !ok || md.Received.After(last) {
		s.last[md.Key] = md.Received
	}
	if _, ok := s.messages[md.Key]; !ok {
		s.messages[md.Key] = make([]string, 0)
	}
	s.messages[md.Key] = append(s.messages[md.Key], name)
}

func (s *DiskStore) writeMetadata(name string, received time.Time, key RecipientKey, msg *ReceivedMessage) error {
	md := &MessageMetadata{
		Received:    received,
		Key:         key,
		MessageFrom: msg.Sender(),
		MessageTo:   msg.Recipients(),
	}

	if bytes, err := json.Marshal(md); err != nil {
		return err
	} else {
		return ioutil.WriteFile(s.jsonPath(name), bytes, 0644)
	}
}

func (s *DiskStore) readMetadata(name string) (*MessageMetadata, error) {
	md := new(MessageMetadata)

	if bytes, err := ioutil.ReadFile(s.jsonPath(name)); err != nil {
		return md, err
	} else {
		err := json.Unmarshal(bytes, md)
		return md, err
	}
}

func (s *DiskStore) readMessage(name string) (*ReceivedMessage, error) {
	metadata, err := s.readMetadata(name)
	if err != nil {
		return nil, err
	}

	data, err := s.Maildir.ReadBytes(name)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(data)
	msg, err := mail.ReadMessage(buf)
	if err != nil {
		return nil, err
	}

	return &ReceivedMessage{
		&message{
			From: metadata.MessageFrom,
			To:   metadata.MessageTo,
			Data: data,
		},
		msg,
	}, nil
}

func (s *DiskStore) jsonPath(name string) string {
	return s.Maildir.path("." + name + ".json")
}

func (s *DiskStore) Add(now time.Time, key RecipientKey, msg *ReceivedMessage) {
	// TODO Update the interface and return errors.
	name, _ := s.Maildir.Write(msg.Contents())

	if _, ok := s.first[key]; !ok {
		s.first[key] = now
		s.messages[key] = make([]string, 0)
	}
	s.last[key] = now
	s.messages[key] = append(s.messages[key], name)

	s.writeMetadata(name, now, key, msg)
}

func (s *DiskStore) Iterate(callback func(RecipientKey, []*ReceivedMessage, time.Time, time.Time) bool) {
	for key, names := range s.messages {
		// Read the messages from the maildir from the message names held
		// by the store.
		// TODO Make this lazy; pass an object that is capable of reading them.
		msgs := make([]*ReceivedMessage, 0, len(names))
		for _, name := range names {
			msg, err := s.readMessage(name)
			if err == nil {
				msgs = append(msgs, msg)
			}
		}

		if callback(key, msgs, s.first[key], s.last[key]) {
			for _, name := range s.messages[key] {
				// TODO Update the interface, and accumulate and return errors.
				s.Maildir.Remove(name)
				os.Remove(s.jsonPath(name))
			}
			delete(s.messages, key)
			delete(s.first, key)
			delete(s.last, key)
		}
	}
}

// A `MessageStore` implementation that holds received messages in memory.
type MemoryStore struct {
	messages map[RecipientKey][]*ReceivedMessage
	*messageTimes
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		make(map[RecipientKey][]*ReceivedMessage),
		&messageTimes{
			make(map[RecipientKey]time.Time),
			make(map[RecipientKey]time.Time),
		},
	}
}

func (s *MemoryStore) Add(now time.Time, key RecipientKey, msg *ReceivedMessage) {
	if _, ok := s.first[key]; !ok {
		s.first[key] = now
		s.messages[key] = make([]*ReceivedMessage, 0)
	}
	s.last[key] = now
	s.messages[key] = append(s.messages[key], msg)
}

func (s *MemoryStore) Iterate(callback func(RecipientKey, []*ReceivedMessage, time.Time, time.Time) bool) {
	for key, msgs := range s.messages {
		if callback(key, msgs, s.first[key], s.last[key]) {
			delete(s.messages, key)
			delete(s.first, key)
			delete(s.last, key)
		}
	}
}

type MessageBuffer struct {
	SoftLimit time.Duration
	HardLimit time.Duration
	Batch     GroupBy // determines how messages are split into summary emails
	Group     GroupBy // determines how messages are grouped within summary emails
	From      string
	Store     MessageStore
}

func NewMessageBuffer(softLimit time.Duration, hardLimit time.Duration, batch GroupBy, group GroupBy, from string) *MessageBuffer {
	return &MessageBuffer{
		softLimit,
		hardLimit,
		batch,
		group,
		from,
		NewMemoryStore(),
	}
}

func (b *MessageBuffer) Flush(force bool) []*SummaryMessage {
	summaries := make([]*SummaryMessage, 0)
	now := nowGetter()
	b.Store.Iterate(func(key RecipientKey, msgs []*ReceivedMessage, first time.Time, last time.Time) bool {
		if !force && b.Store.InRange(now, key, b.SoftLimit, b.HardLimit) {
			return false
		}
		summaries = append(summaries, Summarize(b.Group, b.From, key.Recipient, msgs))
		return true
	})
	return summaries
}

func NormalizeAddress(email string) string {
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return email
	}
	return strings.ToLower(addr.Address)
}

func (b *MessageBuffer) Add(msg *ReceivedMessage) {
	key, _ := b.Batch(msg)
	for _, to := range msg.To {
		recipKey := RecipientKey{key, NormalizeAddress(to)}
		now := nowGetter()
		b.Store.Add(now, recipKey, msg)
	}
}

func (b *MessageBuffer) Stats() *BufferStats {
	uniqueMessages := 0
	allMessages := 0
	now := nowGetter()
	var lastReceived time.Time
	b.Store.Iterate(func(key RecipientKey, msgs []*ReceivedMessage, first time.Time, last time.Time) bool {
		if b.Store.InRange(now, key, b.SoftLimit, b.HardLimit) {
			allMessages += len(msgs)
		}
		if lastReceived.Before(last) {
			lastReceived = last
		}
		uniqueMessages += 1
		return false
	})
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

// Tracks the number of arriving messages in a sliding window, to see whether
// they exceed some limit.
type RateCounter struct {
	Limit  int
	counts *ring.Ring
}

func NewRateCounter(limit int, intervals int) *RateCounter {
	return &RateCounter{limit, ring.New(intervals)}
}

// Tells the `RateCounter` that some number of messages has just arrived.
func (r *RateCounter) Add(messages int) {
	if r.counts.Value == nil {
		r.counts.Value = messages
	} else {
		r.counts.Value = int(r.counts.Value.(int) + messages)
	}
}

// Determines how many messages were received during the window and returns a
// boolean for whether the total exceeds the limit, as well as the total
// itself. Also slides the window forward, forgetting about the count for the
// oldest interval and preparing to count a new interval's worth of messages.
func (r *RateCounter) CheckAndAdvance() (bool, int) {
	var sum int
	r.counts.Do(func(val interface{}) {
		if val != nil {
			sum += val.(int)
		}
	})
	r.counts = r.counts.Next()
	r.counts.Value = 0
	return (r.Limit > 0 && sum >= r.Limit), sum
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
