package main

import (
	"fmt"
	"log"
	"os"
)

var loggerWriter = os.Stderr

// Returns a new `log.Logger` configured to prefix log messages with a
// formatted version of `name`.
func logger(name string) *log.Logger {
	prefix := fmt.Sprintf("[%s] ", name)
	return log.New(loggerWriter, prefix, log.LstdFlags|log.Lmicroseconds)
}
