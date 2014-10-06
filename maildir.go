package main

import (
	"fmt"
	"io/ioutil"
	"net/mail"
	"os"
	"path"
)

// Maildir stores and retrieves messages in maildir format, under a specific
// directory.
type Maildir struct {
	Path string

	messageCounter int
}

// Creates a new maildir, with the necessary subdirectories.
func (m *Maildir) Create() error {
	paths := []string{".", "cur", "new", "tmp"}
	for _, p := range paths {
		if err := os.Mkdir(path.Join(m.Path, p), os.ModeDir|0755); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}

// Returns the next unique name for an incoming message.
func (m *Maildir) NextUniqueName() (string, error) {
	host, err := hostGetter()
	if err != nil {
		return "", err
	}
	m.messageCounter++
	return fmt.Sprintf("%d.%d_%d.%s", nowGetter().Unix(), pidGetter(), m.messageCounter, host), nil
}

// Writes a new message to the maildir, and returns the name of the file it
// wrote along with any errors.
func (m *Maildir) Write(bytes []byte) (string, error) {
	name, err := m.NextUniqueName()
	if err != nil {
		return "", err
	}

	tmpName := path.Join(m.Path, "tmp", name)
	curName := path.Join(m.Path, "cur", name+":2,S")

	if err = ioutil.WriteFile(tmpName, bytes, 0644); err != nil {
		return curName, err
	}

	return path.Base(curName), os.Rename(tmpName, curName)
}

func (m *Maildir) path(name string) string {
	return path.Join(m.Path, "cur", name)
}

// Returns the filenames of the messages in the "cur" directory of the maildir.
func (m *Maildir) List() ([]string, error) {
	files, err := ioutil.ReadDir(path.Join(m.Path, "cur"))
	if err != nil {
		return []string{}, err
	}

	result := make([]string, 0)
	for _, file := range files {
		if !file.IsDir() {
			result = append(result, file.Name())
		}
	}
	return result, nil
}

// Returns the message in the "cur" directory of the maildir, given the
// filename (e.g. from `List()`), as a byte slice.
func (m *Maildir) ReadBytes(name string) ([]byte, error) {
	file, err := os.Open(m.path(name))
	defer file.Close()
	if err != nil {
		return nil, err
	}

	return ioutil.ReadAll(file)
}

// Returns the message in the "cur" directory of the maildir, given the
// filename (e.g. from `List()`), as a parsed `mail.Message`.
func (m *Maildir) Read(name string) (*mail.Message, error) {
	file, err := os.Open(m.path(name))
	defer file.Close()
	if err != nil {
		return nil, err
	}

	return mail.ReadMessage(file)
}

// Removes the message from the "cur" directory of the maildir, given the
// filename (e.g. from `List()`).
func (m *Maildir) Remove(name string) error {
	return os.Remove(m.path(name))
}
