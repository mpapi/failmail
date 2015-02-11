package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
)

// `Maildir` reads, writes, and lists data in a Maildir directory tree. It
// contains few high-level methods for working with messages in a Maildir, and
// directly supports manipulating the directory tree directly.
type Maildir struct {
	Path string

	messageCounter int
}

// `MaildirSubdir` is the type of the names of a Maildir's subdirectories.
type MaildirSubdir string

const (
	MAILDIR_CUR  MaildirSubdir = "cur"
	MAILDIR_NEW                = "new"
	MAILDIR_TMP                = "tmp"
	MAILDIR_META               = ".meta"
)

// Creates a new Maildir, with the necessary subdirectories.
func (m *Maildir) Create() error {
	paths := []string{".", string(MAILDIR_CUR), string(MAILDIR_NEW), string(MAILDIR_TMP), string(MAILDIR_META)}
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

// Writes a new message to the Maildir, and returns the name (without parent
// directory) of the file it wrote along with any errors. The file is written
// to `MAILDIR_TMP` and moved to `MAILDIR_CUR`, as the specification requires.
func (m *Maildir) Write(bytes []byte) (string, error) {
	name, err := m.NextUniqueName()
	if err != nil {
		return "", err
	}

	tmpName := m.path(name, MAILDIR_TMP)
	curName := m.path(name+":2,S", MAILDIR_CUR)

	if err = ioutil.WriteFile(tmpName, bytes, 0644); err != nil {
		return curName, err
	}

	return path.Base(curName), os.Rename(tmpName, curName)
}

// Returns the path (including the root of the Maildir) of a file named `name`
// located under the subdirectory `subdir`.
func (m *Maildir) path(name string, subdir MaildirSubdir) string {
	return path.Join(m.Path, string(subdir), name)
}

// Returns `os.FileInfo` for each file in the subdirectory of the Maildir.
func (m *Maildir) List(subdir MaildirSubdir) ([]os.FileInfo, error) {
	return ioutil.ReadDir(path.Join(m.Path, string(subdir)))
}

// Returns the message in the subdirectory of the Maildir, given the filename
// (without parent directory, e.g. from the `.Name()` method of an
// `os.FileInfo` returned by `List()`), as a byte slice.
func (m *Maildir) ReadBytes(name string, subdir MaildirSubdir) ([]byte, error) {
	return ioutil.ReadFile(m.path(name, subdir))
}

// Removes the message from the subdirectory of the maildir, given the filename
// (without parent directory, e.g. from the `.Name()` method of an
// `os.FileInfo` returned by `List()`).
func (m *Maildir) Remove(name string, subdir MaildirSubdir) error {
	return os.Remove(m.path(name, subdir))
}
