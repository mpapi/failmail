package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
)

type Maildir struct {
	Path string

	messageCounter int
}

func (m *Maildir) Create() error {
	paths := []string{".", "cur", "new", "tmp"}
	for _, p := range paths {
		if err := os.Mkdir(path.Join(m.Path, p), os.ModeDir|0755); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}

func (m *Maildir) NextUniqueName() (string, error) {
	host, err := hostGetter()
	if err != nil {
		return "", err
	}
	m.messageCounter++
	return fmt.Sprintf("%d.%d_%d.%s", nowGetter().Unix(), pidGetter(), m.messageCounter, host), nil
}

func (m *Maildir) Write(bytes []byte) error {
	name, err := m.NextUniqueName()
	if err != nil {
		return err
	}

	tmpName := path.Join(m.Path, "tmp", name)
	curName := path.Join(m.Path, "cur", name+":2,S")

	if err = ioutil.WriteFile(tmpName, bytes, 0644); err != nil {
		return err
	}

	return os.Rename(tmpName, curName)
}
