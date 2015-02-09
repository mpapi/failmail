package main

import (
	"github.com/hut8labs/failmail/configure"
	"io/ioutil"
	"os"
	"testing"
)

func TestConfigStoreDisk(t *testing.T) {
	tmp, err := ioutil.TempDir("", "maildir")
	if err != nil {
		t.Fatalf("unable to create a test directory: %v", err)
	}
	defer os.RemoveAll(tmp)

	config := Defaults()
	configure.ParseArgs(config, "test", []string{"test", "--message-store", tmp})
	if store, err := config.Store(); err != nil {
		t.Errorf("unexpected error getting configured store: %v", err)
	} else if _, ok := store.(*DiskStore); !ok {
		t.Errorf("expected a disk-backed store, got %#v", store)
	}
}

func TestConfigStoreMemory(t *testing.T) {
	config := Defaults()
	configure.ParseArgs(config, "test", []string{"test", "--memory-store"})
	if store, err := config.Store(); err != nil {
		t.Errorf("unexpected error getting configured store: %v", err)
	} else if _, ok := store.(*MemoryStore); !ok {
		t.Errorf("expected a memory-backed store, got %#v", store)
	}
}

func TestConfigStoreErrorNoDiskOrMemory(t *testing.T) {
	config := Defaults()
	configure.ParseArgs(config, "test", []string{"test", "--message-store", ""})
	if _, err := config.Store(); err == nil {
		t.Errorf("expected an error asking for neither memory nor disk stores")
	}
}
