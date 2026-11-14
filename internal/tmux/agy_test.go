package tmux

import "testing"

func TestDetectToolFromCommand_Agy(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"bare agy", "agy", "agy"},
		{"agy uppercase", "AGY", "agy"},
		{"agy full path", "/usr/local/bin/agy --conversation abc-123", "agy"},
		{"agy with exports", "export AGENTDECK_INSTANCE_ID=id1; agy --model gemini-2.5-pro", "agy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectToolFromCommand(tt.command); got != tt.want {
				t.Fatalf("detectToolFromCommand(%q) = %q, want %q", tt.command, got, tt.want)
			}
		})
	}
}

func TestDetectToolFromCommand_Agy_Negative(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{"substring autophagy", "autophagy"},
		{"username directory", "/Users/nagy/bin/zsh"},
		{"argument only", "echo agy --help"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectToolFromCommand(tt.command); got == "agy" {
				t.Fatalf("detectToolFromCommand(%q) = %q, must not match agy", tt.command, got)
			}
		})
	}
}

func TestDefaultRawPatterns_Agy_ReturnsNil(t *testing.T) {
	if raw := DefaultRawPatterns("agy"); raw != nil {
		t.Errorf("DefaultRawPatterns(\"agy\") = %+v, want nil (hook-driven tool)", raw)
	}
}
