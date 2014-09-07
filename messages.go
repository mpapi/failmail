package main

import (
	"bytes"
	"container/ring"
	"fmt"
	"io/ioutil"
	"net/mail"
	"regexp"
	"strings"
	"text/template"
	"time"
)

type OutgoingParts struct {
	From        string
	To          []string
	Bytes       []byte
	Description string
}

type OutgoingMessage interface {
	Parts() *OutgoingParts
}

// A message received from an SMTP client. These get compacted into
// `UniqueMessage`s, many which are then periodically sent via an upstream
// server in a `SummaryMessage`.
type ReceivedMessage struct {
	From       string
	To         []string
	Data       string
	Message    *mail.Message
	bodyCached []byte
}

// Returns the body of the message.
func (r *ReceivedMessage) Body() string {
	if r.bodyCached != nil {
		return string(r.bodyCached)
	} else if r.Message == nil {
		return "[no message body]"
	} else if body, err := ioutil.ReadAll(r.Message.Body); err != nil {
		return "[unreadable message body]"
	} else {
		r.bodyCached = body
		return string(body)
	}
}

func (r *ReceivedMessage) DisplayDate(def string) string {
	if d, err := r.Message.Header.Date(); err != nil {
		return def
	} else {
		return d.Format(time.RFC1123Z)
	}
}

func (r *ReceivedMessage) Parts() *OutgoingParts {
	subj := r.Message.Header.Get("subject")
	return &OutgoingParts{r.From, r.To, []byte(r.Body()), subj}
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

		if date, err := msg.Message.Header.Date(); err == nil {
			if unique.Start.IsZero() || date.Before(unique.Start) {
				unique.Start = date
			}
			if unique.End.IsZero() || date.After(unique.End) {
				unique.End = date
			}
		}
		unique.Body = msg.Body()
		unique.Subject = msg.Message.Header.Get("subject")
		unique.Count += 1
	}
	return result
}

type SummaryMessage struct {
	From             string
	To               []string
	Subject          string
	Date             time.Time
	ReceivedMessages []*ReceivedMessage
	UniqueMessages   []*UniqueMessage
}

func (s *SummaryMessage) writeHeaders(buf *bytes.Buffer) {
	fmt.Fprintf(buf, "From: %s\r\n", s.From)
	fmt.Fprintf(buf, "To: %s\r\n", strings.Join(s.To, ", "))
	fmt.Fprintf(buf, "Subject: %s\r\n", s.Subject)
	fmt.Fprintf(buf, "Date: %s\r\n", s.Date.Format(time.RFC822))
	fmt.Fprintf(buf, "\r\n")
}

func (s *SummaryMessage) Render(tmpl *template.Template) ([]byte, error) {
	buf := new(bytes.Buffer)
	s.writeHeaders(buf)
	err := tmpl.Execute(buf, s)
	return buf.Bytes(), err
}

func (s *SummaryMessage) Bytes() []byte {
	buf := new(bytes.Buffer)
	s.writeHeaders(buf)

	total := 0
	var firstMessageTime time.Time
	var lastMessageTime time.Time

	body := new(bytes.Buffer)
	for i, unique := range s.UniqueMessages {
		fmt.Fprintf(body, "\r\n- Message group %d of %d: %d instances\r\n", i+1, len(s.UniqueMessages), unique.Count)
		fmt.Fprintf(body, "  From %s to %s\r\n\r\n", unique.Start.Format(time.RFC1123Z), unique.End.Format(time.RFC1123Z))
		fmt.Fprintf(body, "Subject: %#v\r\nBody:\r\n%s\r\n", unique.Subject, unique.Body)

		total += unique.Count
		if firstMessageTime.IsZero() || unique.Start.Before(firstMessageTime) {
			firstMessageTime = unique.Start
		}
		if lastMessageTime.IsZero() || unique.End.After(lastMessageTime) {
			lastMessageTime = unique.End
		}
	}

	fmt.Fprintf(buf, "--- Failmail ---\r\n")
	fmt.Fprintf(buf, "Total messages: %d\r\nUnique messages: %d\r\n", total, len(s.UniqueMessages))
	fmt.Fprintf(buf, "Oldest message: %s\r\nNewest message: %s\r\n", firstMessageTime.Format(time.RFC1123Z), lastMessageTime.Format(time.RFC1123Z))
	fmt.Fprintf(buf, "%s", body.Bytes())
	return buf.Bytes()
}

func (s *SummaryMessage) Parts() *OutgoingParts {
	return &OutgoingParts{s.From, s.To, s.Bytes(), s.Subject}
}

func Summarize(group GroupBy, from string, to string, received []*ReceivedMessage) *SummaryMessage {
	result := &SummaryMessage{}
	result.From = from
	result.To = []string{to}
	result.Subject = fmt.Sprintf("[failmail] %s", Plural(len(received), "message", "messages"))
	result.Date = nowGetter()
	result.ReceivedMessages = received
	result.UniqueMessages = Compact(group, received)
	return result
}

type MessageBuffer struct {
	SoftLimit    time.Duration
	HardLimit    time.Duration
	Batch        GroupBy // determines how messages are split into summary emails
	Group        GroupBy // determines how messages are grouped within summary emails
	From         string
	first        map[RecipientKey]time.Time
	last         map[RecipientKey]time.Time
	messages     map[RecipientKey][]*ReceivedMessage
	lastReceived time.Time
}

func NewMessageBuffer(softLimit time.Duration, hardLimit time.Duration, batch GroupBy, group GroupBy, from string) *MessageBuffer {
	return &MessageBuffer{
		softLimit,
		hardLimit,
		batch,
		group,
		from,
		make(map[RecipientKey]time.Time),
		make(map[RecipientKey]time.Time),
		make(map[RecipientKey][]*ReceivedMessage),
		time.Time{},
	}
}

func (b *MessageBuffer) checkWithinLimits(now time.Time, key RecipientKey) bool {
	return now.Sub(b.first[key]) < b.HardLimit && now.Sub(b.last[key]) < b.SoftLimit
}

func (b *MessageBuffer) Flush(force bool) []*SummaryMessage {
	summaries := make([]*SummaryMessage, 0)
	now := nowGetter()
	for key, msgs := range b.messages {
		if !force && b.checkWithinLimits(now, key) {
			continue
		}
		summaries = append(summaries, Summarize(b.Group, b.From, key.Recipient, msgs))
		delete(b.messages, key)
		delete(b.first, key)
		delete(b.last, key)
	}
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
		if _, ok := b.first[recipKey]; !ok {
			b.first[recipKey] = now
			b.messages[recipKey] = make([]*ReceivedMessage, 0)
		}
		b.last[recipKey] = now
		b.messages[recipKey] = append(b.messages[recipKey], msg)
		b.lastReceived = now
	}
}

func (b *MessageBuffer) Stats() *BufferStats {
	allMessages := 0
	now := nowGetter()
	for key, msgs := range b.messages {
		if b.checkWithinLimits(now, key) {
			allMessages += len(msgs)
		}
	}
	return &BufferStats{len(b.messages), allMessages, b.lastReceived}
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
		err := tmpl.Execute(buf, r.Message)
		return buf.String(), err
	}
}
