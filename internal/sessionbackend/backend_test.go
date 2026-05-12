package sessionbackend

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/tmux"
)

func TestNewTmuxBackendNilSession(t *testing.T) {
	if got := NewTmuxBackend(nil); got != nil {
		t.Fatalf("NewTmuxBackend(nil) = %#v, want nil", got)
	}
}

func TestTmuxBackendWrapsSessionMetadata(t *testing.T) {
	sess := tmux.NewSession("test-title", "/tmp/project")
	b := NewTmuxBackend(sess)
	if b == nil {
		t.Fatal("expected non-nil backend")
	}

	if got := b.Name(); got != sess.Name {
		t.Fatalf("Name() = %q, want %q", got, sess.Name)
	}
	if got := b.DisplayName(); got != sess.DisplayName {
		t.Fatalf("DisplayName() = %q, want %q", got, sess.DisplayName)
	}

	b.SetDisplayName("renamed")
	if got := sess.DisplayName; got != "renamed" {
		t.Fatalf("SetDisplayName did not update wrapped session, got %q", got)
	}
}
