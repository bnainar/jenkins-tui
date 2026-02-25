package tui

import (
	"strings"
	"testing"
)

func TestParamsStatusMessageMentionsCtrlA(t *testing.T) {
	msg := paramsStatusMessage()
	if !strings.Contains(msg, "ctrl+a") {
		t.Fatalf("expected status message to mention ctrl+a, got %q", msg)
	}
	if !strings.Contains(msg, "select all/none") {
		t.Fatalf("expected status message to mention select all/none, got %q", msg)
	}
}

func TestHelpTextForScreenParamsMentionsSelectAll(t *testing.T) {
	help := helpTextForScreen(screenParams, false)
	if !strings.Contains(help, "ctrl+a: select all/none") {
		t.Fatalf("expected params help to mention ctrl+a select all/none, got %q", help)
	}
}

func TestHelpTextForScreenDoneAddsRerun(t *testing.T) {
	help := helpTextForScreen(screenDone, true)
	if !strings.Contains(help, "r: rerun failed") {
		t.Fatalf("expected done help to include rerun shortcut, got %q", help)
	}
}
