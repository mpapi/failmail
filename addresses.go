package main

import (
	"regexp"
	"sort"
)

type AddressRewriter struct {
	Source *regexp.Regexp
	Dest   string
}

func (r AddressRewriter) RewriteAll(addresses []string) []string {
	rewritten := make(map[string]bool, 0)
	for _, addr := range addresses {
		rewritten[r.Rewrite(addr)] = true
	}

	results := make([]string, 0, len(rewritten))
	for addr, _ := range rewritten {
		results = append(results, addr)
	}
	sort.Strings(results)

	return results
}

func (r AddressRewriter) Rewrite(address string) string {
	if r.Source == nil || !r.Source.MatchString(address) {
		return address
	}

	res := []byte{}
	for _, s := range r.Source.FindAllStringSubmatchIndex(address, -1) {
		res = r.Source.ExpandString(res, r.Dest, address, s)
	}
	return string(res)
}
