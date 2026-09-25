//go:build !windows

package shared

import "testing"

// The child must start a new session (no controlling terminal), not just a
// new process group: with only Setpgid, SIGHUP on terminal close still
// reaches it.
func TestDetachedAttrSurvivesTerminalClose(t *testing.T) {
	attr := detachedAttr()
	if attr == nil {
		t.Fatal("detachedAttr is nil")
	}
	if !attr.Setsid {
		t.Error("missing Setsid: child keeps the controlling terminal and dies with it")
	}
}
