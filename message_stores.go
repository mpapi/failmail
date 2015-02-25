package main

import (
	"bytes"
	"container/heap"
	"encoding/json"
	"io/ioutil"
	"net/mail"
	"os"
	"sort"
	"time"
)

// `MessageId` uniquely identifies a message to a store implementation. (The
// backing implementation isn't important -- these are returned by `Add()`, and
// can be fed back in to `Remove()`.)
type MessageId interface{}

// `StoredMessage` adds some additional metadata fields to the
// `ReceivedMessage`, used to track messages between separate runs or
// processes.
type StoredMessage struct {
	Id       MessageId
	Received time.Time
	*ReceivedMessage
}

// `MessageStore` is the interface that provides storage and limited retrieval
// of messages for `MessageBuffer`.
type MessageStore interface {
	// Adds a received message, records its receive time, and generates and
	// returns a `MessageId`.
	Add(time.Time, *ReceivedMessage) (MessageId, error)

	// Removes the message with the given `MessageId` from the store.
	Remove(MessageId) error

	// Returns the messages in the store that are newer than the given time.
	MessagesNewerThan(time.Time) ([]*StoredMessage, []error)
}

// `DiskStore` is a `MessageStore` implementation backed by a Maildir on disk.
// It stores metadata (SMTP envelope, receive time) in files in a non-standard
// `.meta` subdirectory of the maildir.
type DiskStore struct {
	Maildir *Maildir
}

// A struct used to serialize SMTP envelope data to a metadata file in the
// Maildir.
type DiskMetadata struct {
	EnvelopeFrom string
	EnvelopeTo   []string
	RedirectedTo []string
}

// `NewDiskStore` creates a new `DiskStore` using `maildir` to back it.
func NewDiskStore(maildir *Maildir) (*DiskStore, error) {
	return &DiskStore{maildir}, nil
}

func (s *DiskStore) Add(now time.Time, msg *ReceivedMessage) (MessageId, error) {
	// Write the contents to the maildir.
	name, err := s.Maildir.Write(msg.Contents())
	if err != nil {
		return nil, err
	}

	// Write the metadata last.
	meta := &DiskMetadata{msg.Sender(), msg.Recipients(), msg.RedirectedTo}
	return MessageId(name), s.writeMetadata(name, now, meta)
}

func (s *DiskStore) Remove(id MessageId) error {
	name := id.(string)

	// Delete the metadata first.
	if err := s.Maildir.Remove(name, MAILDIR_META); err != nil {
		return err
	}
	return s.Maildir.Remove(name, MAILDIR_CUR)
}

func (s *DiskStore) MessagesNewerThan(t time.Time) ([]*StoredMessage, []error) {
	// List the metadata files. These are written last and deleted first, so
	// there should always be a message file for each metadata file (but not
	// necessarily the other way around).
	files, err := s.Maildir.List(MAILDIR_META)
	if err != nil {
		return nil, []error{err}
	}

	result := make([]*StoredMessage, 0, len(files))
	errors := make([]error, 0)
	for _, info := range files {
		if info.ModTime().Before(t) || info.IsDir() {
			continue
		}
		if msg, err := s.readMessage(info.Name()); err != nil {
			errors = append(errors, err)
			continue
		} else {
			result = append(result, &StoredMessage{info.Name(), info.ModTime(), msg})
		}
	}
	if len(errors) == 0 {
		return result, nil
	} else {
		return result, errors
	}
}

// Reads the metadata file corresponding to the message with contents in
// `name`.
func (s *DiskStore) readMetadata(name string) (*DiskMetadata, error) {
	md := new(DiskMetadata)

	if bytes, err := s.Maildir.ReadBytes(name, MAILDIR_META); err != nil {
		return md, err
	} else {
		err := json.Unmarshal(bytes, md)
		return md, err
	}
}

// Writes the metadata to a file in the metadata subdirectory, and sets is mod
// time to the message receive time.
func (s *DiskStore) writeMetadata(name string, now time.Time, metadata *DiskMetadata) error {
	metadataPath := s.Maildir.path(name, MAILDIR_META)
	if bytes, err := json.Marshal(metadata); err != nil {
		return err
	} else if err := ioutil.WriteFile(metadataPath, bytes, 0644); err != nil {
		return err
	} else if err := os.Chtimes(metadataPath, now, now); err != nil {
		return err
	}
	return nil
}

func (s *DiskStore) readMessage(name string) (*ReceivedMessage, error) {
	metadata, err := s.readMetadata(name)
	if err != nil {
		return nil, err
	}

	data, err := s.Maildir.ReadBytes(name, MAILDIR_CUR)
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
			From: metadata.EnvelopeFrom,
			To:   metadata.EnvelopeTo,
			Data: data,
		},
		msg,
		metadata.RedirectedTo,
	}, nil
}

// A `MessageStore` implementation that holds received messages in memory.
type MemoryStore struct {
	messages *TimeOrdered
	counter  int
}

// Implements the interfaces for sort and heap, maintaining a newest-first order.
type TimeOrdered []*StoredMessage

func (t TimeOrdered) Len() int      { return len(t) }
func (t TimeOrdered) Swap(i, j int) { t[i], t[j] = t[j], t[i] }
func (t TimeOrdered) Less(i, j int) bool {
	return t[i].Received.UnixNano() >= t[j].Received.UnixNano()
}
func (t *TimeOrdered) Push(x interface{}) { *t = append(*t, x.(*StoredMessage)) }
func (t *TimeOrdered) Pop() interface{} {
	old := *t
	n := len(old)
	x := old[n-1]
	*t = old[0 : n-1]
	return x
}

func NewMemoryStore() *MemoryStore {
	msgs := &TimeOrdered{}
	heap.Init(msgs)
	return &MemoryStore{msgs, 0}
}

func (s *MemoryStore) Add(now time.Time, msg *ReceivedMessage) (MessageId, error) {
	m := &StoredMessage{MessageId(s.counter), now, msg}
	s.counter += 1
	heap.Push(s.messages, m)
	return m.Id, nil
}

func (s *MemoryStore) Remove(id MessageId) error {
	for i, m := range *s.messages {
		if m.Id == id {
			heap.Remove(s.messages, i)
			break
		}
	}
	return nil
}

func (s *MemoryStore) MessagesNewerThan(t time.Time) ([]*StoredMessage, []error) {
	i := sort.Search(len(*s.messages), func(k int) bool {
		return t.UnixNano() >= (*s.messages)[k].Received.UnixNano()
	})
	result := make([]*StoredMessage, 0, i)
	for _, m := range (*s.messages)[0:i] {
		result = append(result, m)
	}
	return result, nil
}

type MessageWriter struct {
	Store MessageStore
}

func (w *MessageWriter) Run(received <-chan *StorageRequest) error {
	for req := range received {
		_, err := w.Store.Add(nowGetter(), req.Message)
		req.StorageErrors <- err
	}
	return nil
}

// `StorageRequest` instructs a store to write an incoming message, and gives
// the requester the opportunity to block on/check for an error response.
type StorageRequest struct {
	Message       *ReceivedMessage
	StorageErrors chan<- error
}
