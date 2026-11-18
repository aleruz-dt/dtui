package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dynatrace-oss/dtctl/sdk/session"
	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/workspace"
)

func TestFindContextByEnvironment(t *testing.T) {
	cfg := &session.Config{
		Contexts: []session.NamedContext{
			{Name: "prod", Context: session.Context{Environment: "https://abc12345.apps.dynatrace.com"}},
			{Name: "dev", Context: session.Context{Environment: "https://dev67890.apps.dynatrace.com/"}},
		},
	}
	cases := []struct {
		env  string
		want string
		ok   bool
	}{
		{"https://abc12345.apps.dynatrace.com", "prod", true},
		{"https://ABC12345.apps.dynatrace.com/", "prod", true}, // case + trailing slash
		{"abc12345.apps.dynatrace.com", "prod", true},          // bare host
		{"http://dev67890.apps.dynatrace.com", "dev", true},    // scheme ignored
		{"https://other.apps.dynatrace.com", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		name, ok := findContextByEnvironment(cfg, tc.env)
		if name != tc.want || ok != tc.ok {
			t.Errorf("findContextByEnvironment(%q) = %q, %v; want %q, %v", tc.env, name, ok, tc.want, tc.ok)
		}
	}
}

func TestWorkspaceSegmentsDeterministicVariableOrder(t *testing.T) {
	ws := &workspace.Workspace{Segments: []workspace.Segment{
		{Ref: "seg-1", Variables: map[string][]string{
			"zone": {"z1"}, "env": {"prod"}, "region": {"eu"},
		}},
	}}
	refs := workspaceSegments(ws)
	if len(refs) != 1 || refs[0].Ref != "seg-1" {
		t.Fatalf("refs = %+v", refs)
	}
	var names []string
	for _, v := range refs[0].Variables {
		names = append(names, v.Name)
	}
	if got := strings.Join(names, ","); got != "env,region,zone" {
		t.Errorf("variable order = %s, want sorted env,region,zone", got)
	}
}

func TestWorkspaceNotice(t *testing.T) {
	ws, err := workspace.Load(writeWorkspaceFile(t, `
view: pods
timeframe: 24h
segments:
  - payments-prod
  - team-checkout
`))
	if err != nil {
		t.Fatal(err)
	}
	notice := workspaceNotice(ws)
	for _, want := range []string{"workspace ", "2 segments", "last 24h", "view pods"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice %q misses %q", notice, want)
		}
	}

	empty, err := workspace.Load(writeWorkspaceFile(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if got := workspaceNotice(empty); got != "" {
		t.Errorf("empty workspace notice = %q, want none", got)
	}
}

func TestWritePresetListSanitizesValues(t *testing.T) {
	raw := "ERR\x1b[31mOR"
	ws := &workspace.Workspace{Presets: map[string]workspace.Preset{
		"payments-errors": {
			View:     "logs",
			Facets:   []workspace.PresetFacet{{Field: "loglevel", Value: raw}},
			Searches: []string{"a\x07b"},
		},
	}}
	var buf strings.Builder
	writePresetList(&buf, ws, nil)
	out := buf.String()
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\x07") {
		t.Fatalf("raw control characters leaked:\n%q", out)
	}
	if !strings.Contains(out, catalog.SafeText(raw)) {
		t.Errorf("sanitized facet value missing:\n%s", out)
	}
	if !strings.Contains(out, catalog.SafeText("a\x07b")) {
		t.Errorf("sanitized search missing:\n%s", out)
	}
}

func TestListPresetsIsACommand(t *testing.T) {
	need := map[string]bool{"list-presets": false, "delete-preset": false}
	for _, c := range rootCmd.Commands() {
		if _, ok := need[c.Name()]; ok {
			need[c.Name()] = true
		}
	}
	for name, found := range need {
		if !found {
			t.Fatalf("%s must be a cobra subcommand so `dtui --context prod %s` is not parsed as a view", name, name)
		}
	}
}

func TestResolveLaunchNarrowing(t *testing.T) {
	ws, err := workspace.Load(writeWorkspaceFile(t, `
presets:
  payments-errors:
    view: logs
    facets:
      - field: loglevel
        value: ERROR
`))
	if err != nil {
		t.Fatal(err)
	}
	n, err := resolveLaunchNarrowing("", ws, launchFlags{preset: "payments-errors"})
	if err != nil {
		t.Fatal(err)
	}
	if n.view != "logs" || len(n.facets) != 1 || n.facets[0].Value != "ERROR" {
		t.Errorf("preset launch = %+v", n)
	}
	n, err = resolveLaunchNarrowing("logs", ws, launchFlags{preset: "payments-errors", facets: []string{"loglevel=WARN", "k8s.namespace.name=payments"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(n.facets) != 2 || n.facets[0].Value != "WARN" || n.facets[1].Field != "k8s.namespace.name" {
		t.Errorf("cli facet merge = %+v", n.facets)
	}
	if _, err := resolveLaunchNarrowing("pods", ws, launchFlags{preset: "payments-errors"}); err == nil {
		t.Fatal("view mismatch should error")
	}
	if _, err := resolveLaunchNarrowing("", nil, launchFlags{preset: "payments-errors"}); err == nil {
		t.Fatal("missing catalog should error")
	}
	if _, err := resolveLaunchNarrowing("query", nil, launchFlags{facets: []string{"loglevel=ERROR"}}); err == nil {
		t.Fatal("query --facet should error")
	}
	if _, err := resolveLaunchNarrowing("slos", nil, launchFlags{facets: []string{"id=x"}}); err == nil {
		t.Fatal("API-backed --facet should error")
	}
	if _, err := resolveLaunchNarrowing("nav", nil, launchFlags{facets: []string{"id=x"}}); err == nil {
		t.Fatal("nav --facet should error")
	}
}

func TestResolveLaunchHandoffFlags(t *testing.T) {
	n, err := resolveLaunchNarrowing("logs", nil, launchFlags{
		searches: []string{"timeout", " gateway "},
		from:     "2026-01-02T14:00Z",
		to:       "2026-01-02T15:30:00+01:00",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(n.searches) != 2 || n.searches[1] != "gateway" {
		t.Errorf("searches = %q", n.searches)
	}
	wantFrom := time.Date(2026, 1, 2, 14, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 1, 2, 14, 30, 0, 0, time.UTC)
	if !n.from.Equal(wantFrom) || !n.to.Equal(wantTo) || n.tf != "" {
		t.Errorf("window = %v → %v (tf %q)", n.from, n.to, n.tf)
	}

	n, err = resolveLaunchNarrowing("problems", nil, launchFlags{timeframe: "24h"})
	if err != nil || n.tf != "24h" {
		t.Errorf("--timeframe = %+v, %v", n, err)
	}

	n, err = resolveLaunchNarrowing("", nil, launchFlags{dql: "  fetch logs | limit 5\n"})
	if err != nil || n.view != "query" || n.dql != "fetch logs | limit 5" {
		t.Errorf("--dql without a view = %+v, %v", n, err)
	}

	file := filepath.Join(t.TempDir(), "q.dql")
	if err := os.WriteFile(file, []byte("fetch spans\n| limit 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err = resolveLaunchNarrowing("dql", nil, launchFlags{dqlFile: file})
	if err != nil || n.dql != "fetch spans\n| limit 3" {
		t.Errorf("--dql-file = %+v, %v", n, err)
	}

	for name, tc := range map[string]struct {
		view string
		f    launchFlags
	}{
		"search without a view":   {"", launchFlags{searches: []string{"x"}}},
		"empty search":            {"logs", launchFlags{searches: []string{" "}}},
		"search on API view":      {"slos", launchFlags{searches: []string{"x"}}},
		"bad timeframe":           {"logs", launchFlags{timeframe: "yesterday"}},
		"timeframe with from":     {"logs", launchFlags{timeframe: "2h", from: "2026-01-02T14:00Z"}},
		"to without from":         {"logs", launchFlags{to: "2026-01-02T14:00Z"}},
		"from without zone":       {"logs", launchFlags{from: "2026-01-02T14:00"}},
		"to before from":          {"logs", launchFlags{from: "2026-01-02T14:00Z", to: "2026-01-02T13:00Z"}},
		"dql on a table":          {"logs", launchFlags{dql: "fetch logs"}},
		"dql and dql-file":        {"", launchFlags{dql: "fetch logs", dqlFile: file}},
		"dql with a window":       {"", launchFlags{dql: "fetch logs", timeframe: "2h"}},
		"empty dql":               {"query", launchFlags{dql: "   "}},
		"missing dql-file":        {"query", launchFlags{dqlFile: filepath.Join(t.TempDir(), "nope.dql")}},
		"dql with preset":         {"", launchFlags{dql: "fetch logs", preset: "x"}},
		"facet on the query view": {"query", launchFlags{facets: []string{"a=b"}, dql: "fetch logs"}},
	} {
		if _, err := resolveLaunchNarrowing(tc.view, nil, tc.f); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestCheckViewArg(t *testing.T) {
	for _, view := range []string{"nav", "trace", "pods", "logs"} {
		if err := checkViewArg(view); err != nil {
			t.Errorf("%s should take an argument: %v", view, err)
		}
	}
	for _, view := range []string{"home", "query", "dql", "bogus"} {
		if err := checkViewArg(view); err == nil {
			t.Errorf("%s should reject an argument", view)
		}
	}
}

func writeWorkspaceFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), workspace.FileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNewTenantClientNamesAuthLoginOnMissingToken(t *testing.T) {
	cfg := &session.Config{
		CurrentContext: "demo",
		Contexts: []session.NamedContext{
			{Name: "demo", Context: session.Context{Environment: "https://abc12345.apps.dynatrace.com", TokenRef: "missing-token"}},
		},
	}

	_, err := newTenantClient(cfg)

	if err == nil {
		t.Fatal("expected an error for a context whose token does not exist")
	}
	for _, want := range []string{`"demo"`, "dtctl auth login", "--context"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}
