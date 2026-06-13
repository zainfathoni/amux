package tmux

import "testing"

func TestContinueCommandQuotesShellArgs(t *testing.T) {
	got := ContinueCommand("/tmp/with space/that's", "T-'thread'")
	want := "cd '/tmp/with space/that'\\''s' && exec amp threads continue 'T-'\\''thread'\\'''"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
