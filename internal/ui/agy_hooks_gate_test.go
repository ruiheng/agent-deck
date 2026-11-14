package ui

import (
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

func TestShouldAutoInstallAgyHooks(t *testing.T) {
	disabled := false
	enabled := true

	tests := []struct {
		name           string
		workersEnabled bool
		userConfig     *session.UserConfig
		agyCmd         string
		want           bool
	}{
		{
			name:           "nil config, workers on, cmd set -> installs",
			workersEnabled: true,
			userConfig:     nil,
			agyCmd:         "agy",
			want:           true,
		},
		{
			name:           "explicit opt-in -> installs",
			workersEnabled: true,
			userConfig:     &session.UserConfig{Agy: session.AgySettings{HooksEnabled: &enabled}},
			agyCmd:         "agy",
			want:           true,
		},
		{
			name:           "durable opt-out -> skipped",
			workersEnabled: true,
			userConfig:     &session.UserConfig{Agy: session.AgySettings{HooksEnabled: &disabled}},
			agyCmd:         "agy",
			want:           false,
		},
		{
			name:           "workers disabled -> skipped",
			workersEnabled: false,
			userConfig:     nil,
			agyCmd:         "agy",
			want:           false,
		},
		{
			name:           "empty cmd -> skipped",
			workersEnabled: true,
			userConfig:     nil,
			agyCmd:         "",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := homeBackgroundWorkersEnabled
			homeBackgroundWorkersEnabled = tt.workersEnabled
			t.Cleanup(func() { homeBackgroundWorkersEnabled = orig })

			got := shouldAutoInstallAgyHooks(tt.userConfig, tt.agyCmd)
			if got != tt.want {
				t.Fatalf("shouldAutoInstallAgyHooks() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldAutoInstallAgyHooks_DefaultTestModeSkips(t *testing.T) {
	if homeBackgroundWorkersEnabled {
		t.Fatal("TestMain must disable Home background workers")
	}
	if shouldAutoInstallAgyHooks(nil, "agy") {
		t.Fatal("shouldAutoInstallAgyHooks() = true with background workers disabled, want false")
	}
}
