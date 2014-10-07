package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/mail"
	"os"
	"strings"
	"time"
)

// `MessageStore` is the interface that provides storage and limited retrieval
// of messages for `MessageBuffer`.
type MessageStore interface {
	// Adds a message to the store, with the time it was received.
	Add(time.Time, RecipientKey, *ReceivedMessage) error

	// Computes whether the receive time a message (given its key) was within a
	// certain duration of a time. (The first duration is for the time since
	// the message was first seen, and the second is for the time it was most
	// recently seen.)
	InRange(time.Time, RecipientKey, time.Duration, time.Duration) bool

	// Calls a function on each message in the store, removing it from the
	// store if the function returns true.
	Iterate(func(RecipientKey, []*ReceivedMessage, time.Time, time.Time) bool) error
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

func (s *DiskStore) Add(now time.Time, key RecipientKey, msg *ReceivedMessage) error {
	name, err := s.Maildir.Write(msg.Contents())
	if err != nil {
		return err
	}

	if _, ok := s.first[key]; !ok {
		s.first[key] = now
		s.messages[key] = make([]string, 0)
	}
	s.last[key] = now
	s.messages[key] = append(s.messages[key], name)

	return s.writeMetadata(name, now, key, msg)
}

func (s *DiskStore) Iterate(callback func(RecipientKey, []*ReceivedMessage, time.Time, time.Time) bool) error {
	errors := make([]error, 0)
	cleanup := make([]RecipientKey, 0)
	for key, names := range s.messages {
		// Read the messages from the maildir from the message names held
		// by the store.
		// TODO Make this lazy; pass an object that is capable of reading them.
		msgs := make([]*ReceivedMessage, 0, len(names))
		for _, name := range names {
			msg, err := s.readMessage(name)
			if err != nil {
				errors = append(errors, err)
				continue
			} else {
				msgs = append(msgs, msg)
			}
		}

		if callback(key, msgs, s.first[key], s.last[key]) {
			cleanup = append(cleanup, key)
		}
	}

	for _, key := range cleanup {
		for _, name := range s.messages[key] {
			if err := s.Maildir.Remove(name); err != nil {
				errors = append(errors, err)
			}
			if err := os.Remove(s.jsonPath(name)); err != nil {
				errors = append(errors, err)
			}
		}
		delete(s.messages, key)
		delete(s.first, key)
		delete(s.last, key)
	}

	if len(errors) > 0 {
		buf := new(bytes.Buffer)
		for _, err := range errors {
			fmt.Fprintf(buf, "- %s", err.Error())
		}
		return fmt.Errorf("%d errors:\n%s", len(errors), buf.String())
	}
	return nil
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

func (s *MemoryStore) Add(now time.Time, key RecipientKey, msg *ReceivedMessage) error {
	if _, ok := s.first[key]; !ok {
		s.first[key] = now
		s.messages[key] = make([]*ReceivedMessage, 0)
	}
	s.last[key] = now
	s.messages[key] = append(s.messages[key], msg)
	return nil
}

func (s *MemoryStore) Iterate(callback func(RecipientKey, []*ReceivedMessage, time.Time, time.Time) bool) error {
	for key, msgs := range s.messages {
		if callback(key, msgs, s.first[key], s.last[key]) {
			delete(s.messages, key)
			delete(s.first, key)
			delete(s.last, key)
		}
	}
	return nil
}
