package main

import (
	"time"
)

func patchHost(host string, err error) func() {
	orig := hostGetter
	hostGetter = func() (string, error) { return host, err }
	return func() { hostGetter = orig }
}

func patchTime(now time.Time) func() {
	orig := nowGetter
	nowGetter = func() time.Time { return now }
	return func() { nowGetter = orig }
}

func patchPid(pid int) func() {
	orig := pidGetter
	pidGetter = func() int { return pid }
	return func() { pidGetter = orig }
}
