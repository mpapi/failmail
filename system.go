package main

import (
	"os"
	"time"
)

var hostGetter = os.Hostname
var pidGetter = os.Getpid
var nowGetter = time.Now
