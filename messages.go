package main

import (
	"bytes"
	"container/ring"
	"fmt"
	"io/ioutil"
	"net/mail"
	"regexp"
	"sort"
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

/*// The message key is used to determine whether a message is an instance of a*/
/*// group of similar messages. This method returns either:*/
/*//*/
/*// - the value of the X-Failmail-Key header, or*/
/*// - the result of replacing characters in the message subject that match the*/
/*//   regular expression `pattern`*/
/*func (r *ReceivedMessage) Key(pattern *regexp.Regexp) string {*/
/*    var key string*/
/*    if keyHeader, ok := r.Message.Header["X-Failmail-Key"]; ok {*/
/*        key = keyHeader[0]*/
/*    } else {*/
/*        key = pattern.ReplaceAllString(r.Message.Header.Get("Subject"), "#")*/
/*    }*/
/*    sort.Strings(r.To)*/
/*    return fmt.Sprintf("%s;%s", strings.Join(r.To, ","), key)*/
/*}*/

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
		return d.Format(time.RFC822)
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

// Returns for `UniqueMessage` for each distinct key among the received
// messages, using the regular expression `sanitize` to create a representative
// template body for the `UniqueMessage`.
func Compact(group GroupBy, received []*ReceivedMessage) []*UniqueMessage {
	uniques := make(map[string]*UniqueMessage)
	result := make([]*UniqueMessage, 0)
	for _, msg := range received {
		key := group(msg)

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

	for _, msg := range s.ReceivedMessages {
		date := msg.DisplayDate("?")
		subject := msg.Message.Header.Get("subject")
		fmt.Fprintf(buf, "%s: %s\r\n", date, subject)
	}

	for _, unique := range s.UniqueMessages {
		fmt.Fprintf(buf, "\r\n# %d instances\r\n", unique.Count)
		fmt.Fprintf(buf, "* %s - %s\r\n", unique.Start.Format(time.RFC822), unique.End.Format(time.RFC822))
		fmt.Fprintf(buf, "\r\n%s\r\n- %s\r\n%s\r\n", unique.Template, unique.Subject, unique.Body)
	}
	return buf.Bytes()
}

func (s *SummaryMessage) Parts() *OutgoingParts {
	return &OutgoingParts{s.From, s.To, s.Bytes(), s.Subject}
}

func Summarize(group GroupBy, from string, received []*ReceivedMessage) *SummaryMessage {
	result := &SummaryMessage{}
	result.From = from

	recipSet := make(map[string]struct{})
	recips := make([]string, 0)
	for _, msg := range received {
		for _, to := range msg.To {
			if _, ok := recipSet[to]; !ok {
				recipSet[to] = struct{}{}
				recips = append(recips, to)
			}
		}
	}
	sort.Strings(recips)

	result.To = recips
	result.Subject = fmt.Sprintf("[failmail] %s", Plural(len(received), "message", "messages"))
	result.Date = nowGetter()
	result.ReceivedMessages = received
	result.UniqueMessages = Compact(group, received)
	return result
}

type MessageBuffer struct {
	SoftLimit time.Duration
	HardLimit time.Duration
	Batch     GroupBy // determines how messages are split into summary emails
	Group     GroupBy // determines how messages are grouped within summary emails
	first     map[string]time.Time
	last      map[string]time.Time
	messages  map[string][]*ReceivedMessage
}

func NewMessageBuffer(softLimit time.Duration, hardLimit time.Duration, batch GroupBy, group GroupBy) *MessageBuffer {
	return &MessageBuffer{
		softLimit,
		hardLimit,
		batch,
		group,
		make(map[string]time.Time),
		make(map[string]time.Time),
		make(map[string][]*ReceivedMessage),
	}
}

func (b *MessageBuffer) checkWithinLimits(now time.Time, key string) bool {
	return now.Sub(b.first[key]) < b.HardLimit && now.Sub(b.last[key]) < b.SoftLimit
}

func (b *MessageBuffer) Flush(from string) []*SummaryMessage {
	summaries := make([]*SummaryMessage, 0)
	now := nowGetter()
	for key, msgs := range b.messages {
		if b.checkWithinLimits(now, key) {
			continue
		}
		summaries = append(summaries, Summarize(b.Group, from, msgs))
		delete(b.messages, key)
		delete(b.first, key)
		delete(b.last, key)
	}
	return summaries
}

func (b *MessageBuffer) Add(msg *ReceivedMessage) {
	key := b.Batch(msg)
	now := nowGetter()
	if _, ok := b.first[key]; !ok {
		b.first[key] = now
		b.messages[key] = make([]*ReceivedMessage, 0)
	}
	b.last[key] = now
	b.messages[key] = append(b.messages[key], msg)
}

func (b *MessageBuffer) Stats() *BufferStats {
	allMessages := 0
	now := nowGetter()
	for key, msgs := range b.messages {
		if b.checkWithinLimits(now, key) {
			allMessages += len(msgs)
		}
	}
	return &BufferStats{len(b.messages), allMessages}
}

type BufferStats struct {
	ActiveBatches  int
	ActiveMessages int
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

type GroupBy func(*ReceivedMessage) string

func ReplacedSubject(pattern string, replace string) GroupBy {
	re := regexp.MustCompile(pattern)
	return func(r *ReceivedMessage) string {
		return re.ReplaceAllString(r.Message.Header.Get("Subject"), replace)
	}
}

func MatchingSubject(pattern string) GroupBy {
	re := regexp.MustCompile(pattern)
	return func(r *ReceivedMessage) string {
		return strings.Join(re.FindAllString(r.Message.Header.Get("Subject"), -1), "")
	}
}

func SameSubject() GroupBy {
	return func(r *ReceivedMessage) string {
		return strings.TrimSpace(r.Message.Header.Get("Subject"))
	}
}

func Header(header string, defaultValue string) GroupBy {
	return func(r *ReceivedMessage) string {
		if val, ok := r.Message.Header[header]; len(val) == 1 && ok {
			return val[0]
		}
		return defaultValue
	}
}
