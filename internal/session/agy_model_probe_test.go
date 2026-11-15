package session

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const fakeAgyModelsOutput = "\r⠋ Fetching available models...\r⠙ Fetching available models...\rgemini-5-ultra-high         Gemini 5 Ultra (High)\nclaude-opus-6-high           Claude Opus 6 (High)\n"

type fakeAgyCLI struct {
	dir   string
	calls string
}

func installFakeAgy(t *testing.T, body string) *fakeAgyCLI {
	t.Helper()
	dir := t.TempDir()
	f := &fakeAgyCLI{dir: dir, calls: filepath.Join(dir, "calls")}
	fixture := filepath.Join(dir, "models.txt")
	if err := os.WriteFile(fixture, []byte(fakeAgyModelsOutput), 0o644); err != nil {
		t.Fatal(err)
	}
	f.writeStub(t, "agy", body)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("FAKE_AGY_FIXTURE", fixture)
	t.Setenv("FAKE_AGY_CALLS", f.calls)
	t.Setenv(modelProbeTestEnv, "1")
	restoreCfg := resetUserConfigCache(t, &UserConfig{})
	ResetModelProbeMemo()
	t.Cleanup(func() {
		restoreCfg()
		ResetModelProbeMemo()
	})
	return f
}

func (f *fakeAgyCLI) writeStub(t *testing.T, name, body string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo '1.5.9'; exit 0; fi\n" +
		"echo \"$*\" >> \"$FAKE_AGY_CALLS\"\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (f *fakeAgyCLI) probeCalls(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile(f.calls)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

func TestParseAgyModels(t *testing.T) {
	sample := "\r⠋ Fetching available models...\r⠙ Fetching available models...\r" +
		"gemini-4-carbon-medium         Gemini 4 Carbon (Medium)\n" +
		"claude-opus-5-5-high           Claude Opus 5.5 (High)\n" +
		"\x1b[2Kgemini-4-carbon-high\x1b[0m\tGemini 4 Carbon (High)\r\n" +
		"gemini-4-carbon-medium         Gemini 4 Carbon (Medium)\n" +
		"gemini-4-hydrogen-xhigh\n"

	got, err := parseAgyModels([]byte(sample))
	if err != nil {
		t.Fatalf("parseAgyModels unexpected error: %v", err)
	}
	want := []string{
		"gemini-4-carbon-medium",
		"claude-opus-5-5-high",
		"gemini-4-carbon-high",
		"gemini-4-hydrogen-xhigh",
	}
	if !slices.Equal(got.Models, want) {
		t.Fatalf("models = %v, want %v", got.Models, want)
	}

	invalidCases := map[string]string{
		"empty":        "",
		"whitespace":   "   \n\t\r\n",
		"spinner only": "\r⠋ Fetching available models...\r⠙ Fetching available models...\r\n",
		"prose header": "Available models:\ngemini-4-carbon-medium         Gemini 4 Carbon (Medium)\n",
		"table header": "MODEL  NAME\ngemini-4-carbon-medium         Gemini 4 Carbon (Medium)\n",
		"id header":    "id\tname\ngemini-4-carbon-medium\tGemini 4 Carbon\n",
		"glog line":    "E1114 21:39:12.410531       1 main.go:784] Failed to redirect output\n",
		"json output":  "{\"models\":[]}\n",
		"bare header":  "header\n",
		"unsafe chars": "gemini;rm -rf  Bad\n",
	}
	for name, input := range invalidCases {
		if _, err := parseAgyModels([]byte(input)); err == nil {
			t.Errorf("%s: expected error for input %q, got nil", name, input)
		}
	}
}

func TestAgyModelProbe_MergesProbedAheadOfStaticFloor(t *testing.T) {
	installFakeAgy(t, `cat "$FAKE_AGY_FIXTURE"`)

	staticFloor := staticModelIDsForTool("agy")
	if len(staticFloor) != 22 {
		t.Fatalf("staticModelIDsForTool(agy) has %d entries, want 22", len(staticFloor))
	}
	if staticFloor[0] != "gemini-4-carbon-medium" || staticFloor[len(staticFloor)-1] != "gpt-oss-120b-medium" {
		t.Fatalf("unexpected agy static floor bounds: %v", staticFloor)
	}

	models := KnownModelIDsForTool("agy")
	wantPrefix := []string{"gemini-5-ultra-high", "claude-opus-6-high"}
	if len(models) < len(wantPrefix) || !slices.Equal(models[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("agy models = %v, want probed prefix %v", models, wantPrefix)
	}
	for _, id := range staticFloor {
		if !slices.Contains(models, id) {
			t.Fatalf("static fallback id %q missing from probed merge %v", id, models)
		}
	}

	// [models.agy] extra prepends ahead of probed models.
	setModelsConfig(t, ModelsSettings{
		Agy: ModelListSettings{Extra: []string{"custom-agy-model"}},
	})
	withExtra := KnownModelIDsForTool("agy")
	if want := []string{"custom-agy-model", "gemini-5-ultra-high", "claude-opus-6-high"}; !slices.Equal(withExtra[:len(want)], want) {
		t.Fatalf("agy models with extra = %v, want prefix %v", withExtra, want)
	}

	// [models.agy] list pins suggestions without executing the CLI.
	fList := installFakeAgy(t, `cat "$FAKE_AGY_FIXTURE"`)
	setModelsConfig(t, ModelsSettings{
		Agy: ModelListSettings{List: []string{"pinned-agy-1", "pinned-agy-2"}},
	})
	if pinned := KnownModelIDsForTool("agy"); !slices.Equal(pinned, []string{"pinned-agy-1", "pinned-agy-2"}) {
		t.Fatalf("pinned agy list = %v, want [pinned-agy-1 pinned-agy-2]", pinned)
	}
	if n := fList.probeCalls(t); n != 0 {
		t.Fatalf("pinned list still executed CLI %d times", n)
	}

	// [models] probe = false skips the CLI while keeping extra + static floor.
	fOff := installFakeAgy(t, `cat "$FAKE_AGY_FIXTURE"`)
	off := false
	setModelsConfig(t, ModelsSettings{
		Probe: &off,
		Agy:   ModelListSettings{Extra: []string{"custom-agy-model"}},
	})
	gotOff := KnownModelIDsForTool("agy")
	wantOff := append([]string{"custom-agy-model"}, staticFloor...)
	if !slices.Equal(gotOff, wantOff) {
		t.Fatalf("probe=false agy models = %v, want %v", gotOff, wantOff)
	}
	if n := fOff.probeCalls(t); n != 0 {
		t.Fatalf("probe=false still executed CLI %d times", n)
	}
}

func TestAgyModelProbe_UsesConfiguredAgyCommandAndFallsBackOnFailure(t *testing.T) {
	f := installFakeAgy(t, `pwd > "$FAKE_AGY_CALLS.pwd"; printf '%s' "$AGY_DATA_DIR" > "$FAKE_AGY_CALLS.env"; cat "$FAKE_AGY_FIXTURE"`)
	if err := os.Rename(filepath.Join(f.dir, "agy"), filepath.Join(f.dir, "agy-dev")); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	project := t.TempDir()
	t.Chdir(project)

	cfg := &UserConfig{}
	cfg.Agy.Command = "AGY_DATA_DIR=~/custom agy-dev"
	t.Cleanup(resetUserConfigCache(t, cfg))

	if got := agyProbeBinary(); got != "agy-dev" {
		t.Fatalf("agyProbeBinary() = %q, want agy-dev", got)
	}
	models := KnownModelIDsForTool("agy")
	if len(models) == 0 || models[0] != "gemini-5-ultra-high" {
		t.Fatalf("agy models = %v, want probed gemini-5-ultra-high first", models)
	}
	pwdData, err := os.ReadFile(f.calls + ".pwd")
	if err != nil {
		t.Fatal(err)
	}
	gotDir, _ := filepath.EvalSymlinks(strings.TrimSpace(string(pwdData)))
	wantDir, _ := filepath.EvalSymlinks(home)
	if gotDir != wantDir {
		t.Fatalf("probe ran in %q, want home %q", gotDir, wantDir)
	}
	envData, err := os.ReadFile(f.calls + ".env")
	if err != nil {
		t.Fatal(err)
	}
	if gotEnv, wantEnv := string(envData), filepath.Join(home, "custom"); gotEnv != wantEnv {
		t.Fatalf("AGY_DATA_DIR = %q, want %q", gotEnv, wantEnv)
	}

	for name, body := range map[string]string{
		"non-zero exit": `echo 'Error: failed' >&2; exit 1`,
		"malformed":     `echo 'Available models:'`,
	} {
		t.Run(name, func(t *testing.T) {
			installFakeAgy(t, body)
			if got := KnownModelIDsForTool("agy"); !slices.Equal(got, staticModelIDsForTool("agy")) {
				t.Fatalf("got %v, want static fallback %v", got, staticModelIDsForTool("agy"))
			}
			assertFailureCachedBriefly(t, "agy")
		})
	}
}
