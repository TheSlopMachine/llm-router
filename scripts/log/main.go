package main

import (
	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// log dumps the dev backend log and follows it like tail -f on a TTY.
// Non-interactive use (pipes, agents) prints the tail and exits: set
// FOLLOW=1 to force follow or FOLLOW=0 to force dump-and-exit, and
// LINES=N to size the initial tail (default 100).
func main() {
	logPath := shared.DefaultBackendLog()
	lines := shared.LinesFromEnv(100)
	if err := shared.DumpAndFollow(logPath, lines, shared.FollowFromEnv()); err != nil {
		shared.Failf("%v", err)
	}
}
