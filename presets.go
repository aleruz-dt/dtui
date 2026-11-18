package main

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/adrg/xdg"
	"github.com/spf13/cobra"

	"github.com/dynatrace-oss/dtui/internal/tui"
	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/workspace"
)

var listPresetsCmd = &cobra.Command{
	Use:   "list-presets",
	Short: "Print named facet presets and exit",
	Long:  "Print presets from .dtui.yaml and the active context's personal presets (~/.local/state/dtui/presets/<context>.yaml). Does not need a TTY.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, err := presetContext()
		if err != nil {
			return err
		}
		project, personal, warnings := loadPresetFiles(ctx)
		writeLoadWarnings(cmd.ErrOrStderr(), warnings)
		fmt.Fprintf(cmd.OutOrStdout(), "context: %s\n", catalog.SafeText(ctx))
		writePresetList(cmd.OutOrStdout(), workspace.MergePresetCatalog(project, personal), personal)
		return nil
	},
}

var deletePresetCmd = &cobra.Command{
	Use:               "delete-preset NAME",
	Short:             "Delete a personal named facet preset and exit",
	Long:              "Removes NAME from the active context's personal presets (~/.local/state/dtui/presets/<context>.yaml). Workspace presets in .dtui.yaml are not edited. Does not need a TTY.",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: personalPresetCompletion,
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		ctx, err := presetContext()
		if err != nil {
			return err
		}
		project, personal, warnings := loadPresetFiles(ctx)
		writeLoadWarnings(cmd.ErrOrStderr(), warnings)
		if _, ok := personal.LookupPreset(name); !ok {
			if _, inWS := project.LookupPreset(name); inWS {
				return fmt.Errorf("preset %q is in .dtui.yaml — edit that file (context %s has no personal %q)", name, ctx, name)
			}
			return fmt.Errorf("unknown personal preset %q in context %s", name, ctx)
		}
		if err := workspace.DeletePreset(personalPresetsPath(ctx), name); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "deleted preset %s (context %s)\n", name, ctx)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listPresetsCmd)
	rootCmd.AddCommand(deletePresetCmd)
}

// presetContext is the context a preset subcommand acts on: the same one a
// launch would use (applyWorkspaceContext), so list, save and delete agree
// with the TUI.
func presetContext() (string, error) {
	cfg, err := loadConfig()
	if err != nil {
		return "", err
	}
	ws, _ := loadWorkspace()
	applyWorkspaceContext(cfg, ws)
	if cfg.CurrentContext == "" {
		return "", fmt.Errorf("no current context — personal presets are per context; pick one with --context <name>")
	}
	return cfg.CurrentContext, nil
}

func loadPresetFiles(ctx string) (project, personal *workspace.Workspace, warnings []string) {
	project, warnings = loadWorkspace()
	personal, personalWarn := loadPersonalPresets(ctx)
	if personalWarn != "" {
		warnings = append(warnings, personalWarn)
	}
	return project, personal, warnings
}

func loadPresetCatalog(ctx string) (*workspace.Workspace, []string) {
	project, personal, warnings := loadPresetFiles(ctx)
	return workspace.MergePresetCatalog(project, personal), warnings
}

func writeLoadWarnings(w io.Writer, warnings []string) {
	for _, msg := range warnings {
		fmt.Fprintln(w, msg)
	}
}

// launchFlags is the raw CLI narrowing of the starting page.
type launchFlags struct {
	preset    string
	facets    []string
	searches  []string
	timeframe string
	from, to  string
	dql       string
	dqlFile   string
}

// launchNarrowing is the resolved launch input applied to the first page.
// Empty fields mean "not set"; a non-zero from wins over tf.
type launchNarrowing struct {
	view     string
	tf       string
	from, to time.Time
	facets   []catalog.Facet
	searches []string
	dql      string
}

func resolveLaunchNarrowing(cliView string, ws *workspace.Workspace, f launchFlags) (launchNarrowing, error) {
	var n launchNarrowing
	n.view = cliView
	presetName := f.preset

	if presetName != "" {
		if ws == nil || len(ws.Presets) == 0 {
			return n, fmt.Errorf("preset %q: no presets in .dtui.yaml or this context's personal presets", presetName)
		}
		p, ok := ws.LookupPreset(presetName)
		if !ok {
			avail := ws.PresetNames()
			if len(avail) == 0 {
				return n, fmt.Errorf("unknown preset %q (workspace has none)", presetName)
			}
			return n, fmt.Errorf("unknown preset %q (available: %s)", presetName, strings.Join(avail, ", "))
		}
		presetView := canonicalViewName(p.View)
		if catalog.Lookup(presetView) == nil && !isBespokeView(presetView) {
			return n, fmt.Errorf("preset %q: unknown view %q", presetName, p.View)
		}
		if err := requireFacetTable(presetView); err != nil {
			return n, fmt.Errorf("preset %q: %w", presetName, err)
		}
		if n.view != "" {
			if canonicalViewName(n.view) != presetView {
				return n, fmt.Errorf("preset %q is for view %s, not %s", presetName, p.View, n.view)
			}
		} else {
			n.view = presetView
		}
		n.tf = p.Timeframe
		n.facets = catalogFacets(p.Facets)
		n.searches = append([]string{}, p.Searches...)
	}

	extra, err := parseFacetFlags(f.facets)
	if err != nil {
		return n, err
	}
	n.facets = mergeFacets(n.facets, extra)
	searches, err := parseSearchFlags(f.searches)
	if err != nil {
		return n, err
	}
	n.searches = append(n.searches, searches...)
	if presetName == "" && len(extra) > 0 && n.view == "" {
		return n, fmt.Errorf("--facet requires a view argument (e.g. dtui logs --facet loglevel=ERROR)")
	}
	if presetName == "" && len(searches) > 0 && n.view == "" {
		return n, fmt.Errorf("--search requires a view argument (e.g. dtui logs --search timeout)")
	}
	if n.view != "" && (presetName != "" || len(n.facets) > 0 || len(n.searches) > 0) {
		if err := requireFacetTable(n.view); err != nil {
			return n, err
		}
	}

	if err := resolveLaunchWindow(&n, f); err != nil {
		return n, err
	}
	if err := resolveLaunchDQL(&n, f); err != nil {
		return n, err
	}

	if n.view != "" && catalog.Lookup(n.view) == nil && !isBespokeView(n.view) && !isNavView(n.view) {
		return n, fmt.Errorf("unknown view %q", n.view)
	}
	return n, nil
}

// resolveLaunchWindow applies --timeframe or --from/--to; either wins over a
// preset's timeframe.
func resolveLaunchWindow(n *launchNarrowing, f launchFlags) error {
	if f.timeframe != "" && (f.from != "" || f.to != "") {
		return fmt.Errorf("--timeframe and --from/--to are mutually exclusive")
	}
	if f.timeframe != "" {
		if _, ok := catalog.ParseTimeframe(f.timeframe); !ok {
			return fmt.Errorf("--timeframe %q: expected a relative window like 45m, 2h, 7d", f.timeframe)
		}
		n.tf = f.timeframe
		return nil
	}
	if f.to != "" && f.from == "" {
		return fmt.Errorf("--to requires --from")
	}
	if f.from == "" {
		return nil
	}
	from, err := parseLaunchTime("--from", f.from)
	if err != nil {
		return err
	}
	var to time.Time
	if f.to != "" {
		if to, err = parseLaunchTime("--to", f.to); err != nil {
			return err
		}
		if !to.After(from) {
			return fmt.Errorf("--to %s must be after --from %s", f.to, f.from)
		}
	}
	n.tf, n.from, n.to = "", from, to
	return nil
}

// launchTimeLayouts are the accepted --from/--to forms. A zone is required:
// a handed-over command must mean the same instant on every machine.
var launchTimeLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04Z07:00"}

func parseLaunchTime(flag, s string) (time.Time, error) {
	for _, layout := range launchTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%s %q: expected RFC 3339 with a zone (2026-01-02T15:04Z or 2026-01-02T15:04:05+01:00)", flag, s)
}

// resolveLaunchDQL applies --dql / --dql-file: they open the query editor
// with the query already running.
func resolveLaunchDQL(n *launchNarrowing, f launchFlags) error {
	if f.dql == "" && f.dqlFile == "" {
		return nil
	}
	if f.dql != "" && f.dqlFile != "" {
		return fmt.Errorf("--dql and --dql-file are mutually exclusive")
	}
	if f.preset != "" {
		return fmt.Errorf("--dql cannot be combined with --preset")
	}
	if n.view != "" && canonicalViewName(n.view) != "query" {
		return fmt.Errorf("--dql opens the query view, not %s", n.view)
	}
	if n.tf != "" || !n.from.IsZero() {
		return fmt.Errorf("--dql ignores --timeframe/--from/--to; put from:/to: in the query instead")
	}
	dql := f.dql
	if f.dqlFile != "" {
		data, err := os.ReadFile(f.dqlFile)
		if err != nil {
			return fmt.Errorf("--dql-file: %w", err)
		}
		dql = string(data)
	}
	if strings.TrimSpace(dql) == "" {
		return fmt.Errorf("--dql: query is empty")
	}
	n.view, n.dql = "query", strings.TrimSpace(dql)
	return nil
}

func parseSearchFlags(flags []string) ([]string, error) {
	out := make([]string, 0, len(flags))
	for _, s := range flags {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, fmt.Errorf("--search: term is empty")
		}
		out = append(out, s)
	}
	return out, nil
}

// requireFacetTable rejects views that cannot apply server-side facets: the
// bespoke screens, the navigator, and API-backed catalog specs.
func requireFacetTable(name string) error {
	if isBespokeView(name) || isNavView(name) {
		return fmt.Errorf("view %q is not a catalog table", name)
	}
	spec := catalog.Lookup(name)
	if spec == nil {
		return fmt.Errorf("unknown view %q", name)
	}
	if spec.API != "" {
		return fmt.Errorf("view %q is API-backed and has no DQL facets", name)
	}
	return nil
}

func canonicalViewName(name string) string {
	switch name {
	case "dql":
		return "query"
	case "smartscape", "navigator":
		return "nav"
	}
	if spec := catalog.Lookup(name); spec != nil {
		return spec.Name
	}
	return name
}

func catalogFacets(in []workspace.PresetFacet) []catalog.Facet {
	out := make([]catalog.Facet, 0, len(in))
	for _, f := range in {
		out = append(out, catalog.Facet{
			Field:    f.Field,
			Value:    f.Value,
			Tokens:   f.Tokens,
			MapField: f.MapField,
			MapKey:   f.MapKey,
		})
	}
	return out
}

func parseFacetFlags(flags []string) ([]catalog.Facet, error) {
	var out []catalog.Facet
	for _, s := range flags {
		field, value, ok := strings.Cut(s, "=")
		field, value = strings.TrimSpace(field), strings.TrimSpace(value)
		if !ok || field == "" || value == "" {
			return nil, fmt.Errorf("--facet %q: expected field=value", s)
		}
		if !catalog.SafeFacetField(field) {
			return nil, fmt.Errorf("--facet %q: field contains unsafe characters", s)
		}
		out = append(out, catalog.Facet{Field: field, Value: value})
	}
	return out, nil
}

// mergeFacets ANDs extra onto base; a CLI facet whose Field matches a preset
// facet replaces it (CLI wins on the same field).
func mergeFacets(base, extra []catalog.Facet) []catalog.Facet {
	out := append([]catalog.Facet{}, base...)
	for _, e := range extra {
		replaced := false
		for i, b := range out {
			if b.Field == e.Field {
				out[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, e)
		}
	}
	return out
}

func writePresetList(w io.Writer, merged, personal *workspace.Workspace) {
	if merged == nil || len(merged.Presets) == 0 {
		fmt.Fprintln(w, "no presets")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tVIEW\tSOURCE\tNARROWING")
	for _, name := range merged.PresetNames() {
		p := merged.Presets[name]
		src := "workspace"
		if personal != nil {
			if _, ok := personal.LookupPreset(name); ok {
				src = "personal"
			}
		}
		var parts []string
		for _, f := range p.Facets {
			parts = append(parts, catalog.SafeText(f.Field)+"="+catalog.SafeText(f.Value))
		}
		for _, s := range p.Searches {
			parts = append(parts, "/"+catalog.SafeText(s))
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, p.View, src, strings.Join(parts, ", "))
	}
	_ = tw.Flush()
}

func namedPresets(ws *workspace.Workspace, personal bool) []tui.NamedPreset {
	if ws == nil {
		return nil
	}
	out := make([]tui.NamedPreset, 0, len(ws.Presets))
	for _, name := range ws.PresetNames() {
		p := ws.Presets[name]
		out = append(out, tui.NamedPreset{
			Name:      name,
			View:      p.View,
			Timeframe: p.Timeframe,
			Facets:    catalogFacets(p.Facets),
			Searches:  append([]string{}, p.Searches...),
			Personal:  personal,
		})
	}
	return out
}

func mergedNamedPresets(project, personal *workspace.Workspace) []tui.NamedPreset {
	merged := workspace.MergePresetCatalog(project, personal)
	out := namedPresets(merged, false)
	personalNames := map[string]bool{}
	if personal != nil {
		for _, name := range personal.PresetNames() {
			personalNames[name] = true
		}
	}
	for i, p := range out {
		out[i].Personal = personalNames[p.Name]
	}
	return out
}

func savePersonalPreset(path string) func(tui.NamedPreset) error {
	return func(p tui.NamedPreset) error {
		facets := make([]workspace.PresetFacet, 0, len(p.Facets))
		for _, f := range p.Facets {
			facets = append(facets, workspace.PresetFacet{
				Field: f.Field, Value: f.Value, Tokens: f.Tokens,
				MapField: f.MapField, MapKey: f.MapKey,
			})
		}
		return workspace.SavePreset(path, p.Name, workspace.Preset{
			View: p.View, Timeframe: p.Timeframe, Facets: facets, Searches: p.Searches,
		})
	}
}

func deletePersonalPreset(path string) func(name string) error {
	return func(name string) error {
		return workspace.DeletePreset(path, name)
	}
}

// personalPresetsPath is the personal preset file of one dtctl context:
// presets carry tenant-specific values (namespaces, hosts), so each context
// keeps its own, as navigation history does.
func personalPresetsPath(ctx string) string {
	return filepath.Join(xdg.StateHome, "dtui", "presets", presetFileName(ctx))
}

// presetFileName escapes a context name into one safe path element: no
// separators, and no leading dot (so "." or ".." cannot walk the tree).
func presetFileName(ctx string) string {
	name := url.PathEscape(ctx)
	if strings.HasPrefix(name, ".") {
		name = "%2E" + name[1:]
	}
	return name + ".yaml"
}

func loadPersonalPresets(ctx string) (*workspace.Workspace, string) {
	path := personalPresetsPath(ctx)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, ""
		}
		return nil, "personal presets ignored: " + err.Error()
	}
	w, err := workspace.Load(path)
	if err != nil {
		return nil, "personal presets ignored: " + err.Error()
	}
	return w, ""
}
