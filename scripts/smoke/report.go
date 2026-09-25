package main

import (
	"fmt"
	"strings"
	"time"
)

// Status of one check.
const (
	pass = "PASS"
	fail = "FAIL"
	skip = "SKIP"
)

type result struct {
	provider string
	model    string
	target   string
	status   string
	reason   string
	latency  time.Duration
}

type report struct {
	results []result
}

func (r *report) add(provider, model, target, status, reason string, latency time.Duration) {
	r.results = append(r.results, result{provider, model, target, status, reason, latency})
}

func (r *report) print() {
	current := ""
	for _, res := range r.results {
		if res.provider != current {
			current = res.provider
			fmt.Printf("== %s\n", current)
		}
		line := fmt.Sprintf("  %-18s %-22s %s", res.target, res.model, res.status)
		if res.reason != "" {
			line += fmt.Sprintf(" (%s)", res.reason)
		}
		if res.status == pass {
			line += fmt.Sprintf(" [%s]", res.latency.Round(time.Millisecond))
		}
		fmt.Println(strings.TrimSpace(line))
	}
	var passed, failed, skipped int
	for _, res := range r.results {
		switch res.status {
		case pass:
			passed++
		case fail:
			failed++
		default:
			skipped++
		}
	}
	fmt.Printf("summary: %d passed, %d failed, %d skipped\n", passed, failed, skipped)
}

func (r *report) failed() bool {
	for _, res := range r.results {
		if res.status == fail {
			return true
		}
	}
	return false
}
