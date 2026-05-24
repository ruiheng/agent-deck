//go:build windows

package tmux

// EmitITermBadgeViaTty is a no-op on native Windows. The Unix implementation
// relies on /dev/tty and iTerm2 OSC passthrough.
func EmitITermBadgeViaTty(title string, configEnabled bool) {
}
