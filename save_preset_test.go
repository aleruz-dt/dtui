package main

import (
	"bytes"
	"testing"

	"github.com/adrg/xdg"

	"github.com/dynatrace-oss/dtui/internal/workspace"
)

// runSavePreset runs `dtui save-preset args...` against a throwaway state
// dir and no workspace file, returning stdout and the error.
func runSavePreset(t *testing.T, args ...string) (string, error) {
	t.Helper()
	saveLaunch = launchFlags{}
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(append([]string{"save-preset"}, args...))
	t.Cleanup(func() { rootCmd.SetArgs(nil); rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	err := rootCmd.Execute()
	return out.String(), err
}

func isolatePresetState(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	xdg.Reload()
	t.Cleanup(xdg.Reload)
	t.Chdir(t.TempDir())
}

func TestSavePresetWritesWhatLaunchReads(t *testing.T) {
	isolatePresetState(t)
	out, err := runSavePreset(t, "payments-errors", "logs", "--facet", "loglevel=ERROR", "--search", "payment", "--timeframe", "2h")
	if err != nil {
		t.Fatalf("save-preset: %v\n%s", err, out)
	}
	w, err := workspace.Load(personalPresetsPath())
	if err != nil {
		t.Fatal(err)
	}
	p, ok := w.LookupPreset("payments-errors")
	if !ok || p.View != "logs" || p.Timeframe != "2h" || len(p.Facets) != 1 || p.Facets[0].Value != "ERROR" || p.Searches[0] != "payment" {
		t.Fatalf("saved = %+v (found %v)", p, ok)
	}
	nav, err := resolveLaunch("", "", w, launchFlags{preset: "payments-errors"})
	if err != nil || nav.Target != "logs" || len(nav.Facets) != 1 || nav.Searches[0] != "payment" {
		t.Fatalf("--preset round trip = %+v, %v", nav, err)
	}
}

func TestSavePresetReplacesLikeW(t *testing.T) {
	isolatePresetState(t)
	if _, err := runSavePreset(t, "errs", "logs", "--facet", "loglevel=ERROR"); err != nil {
		t.Fatal(err)
	}
	if _, err := runSavePreset(t, "errs", "logs", "--facet", "loglevel=WARN"); err != nil {
		t.Fatal(err)
	}
	w, _ := workspace.Load(personalPresetsPath())
	if p, _ := w.LookupPreset("errs"); p.Facets[0].Value != "WARN" {
		t.Errorf("value = %q, want the second save to replace the first", p.Facets[0].Value)
	}
}

func TestSavePresetRejectsWhatAPresetCannotHold(t *testing.T) {
	isolatePresetState(t)
	for name, args := range map[string][]string{
		"no narrowing":    {"x", "logs"},
		"bad name":        {"Not_Kebab", "logs", "--facet", "a=b"},
		"unknown view":    {"x", "nope", "--facet", "a=b"},
		"non-facet view":  {"x", "query", "--search", "a"},
		"unsafe field":    {"x", "logs", "--facet", "a`|b=c"},
		"bad timeframe":   {"x", "logs", "--facet", "a=b", "--timeframe", "soon"},
		"no launch flags": {"x", "logs", "--facet", "a=b", "--lens", "genai"},
	} {
		if _, err := runSavePreset(t, args...); err == nil {
			t.Errorf("%s: save-preset %v succeeded", name, args)
		}
	}
}
