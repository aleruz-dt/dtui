package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/adrg/xdg"
	"github.com/spf13/cobra"

	"github.com/dynatrace-oss/dtui/internal/tui"
	"github.com/dynatrace-oss/dtui/internal/tui/catalog"
	"github.com/dynatrace-oss/dtui/internal/workspace"
)

var listPresetsCmd = &cobra.Command{
	Use:   "list-presets",
	Short: "Print named facet presets and exit",
	Long:  "Print presets from .dtui.yaml and ~/.local/state/dtui/presets.yaml. Does not need a TTY.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		presets, _ := loadPresetCatalog()
		writePresetList(cmd.OutOrStdout(), presets)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listPresetsCmd)
}

func loadPresetCatalog() (*workspace.Workspace, []string) {
	ws, warnings := loadWorkspace()
	personal, personalWarn := loadPersonalPresets()
	if personalWarn != "" {
		warnings = append(warnings, personalWarn)
	}
	return workspace.MergePresetCatalog(ws, personal), warnings
}

// launchNarrowing is the resolved --preset / --facet input applied to the
// first catalog table. Empty fields mean "not set".
type launchNarrowing struct {
	view     string
	tf       string
	facets   []catalog.Facet
	searches []string
}

func resolveLaunchNarrowing(cliView string, ws *workspace.Workspace, presetName string, facetFlags []string) (launchNarrowing, error) {
	var n launchNarrowing
	n.view = cliView

	if presetName != "" {
		if ws == nil || len(ws.Presets) == 0 {
			return n, fmt.Errorf("preset %q: no presets in .dtui.yaml or ~/.local/state/dtui/presets.yaml", presetName)
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
		if isBespokeView(presetView) || isNavView(presetView) {
			return n, fmt.Errorf("preset %q: view %q is not a catalog table", presetName, p.View)
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

	extra, err := parseFacetFlags(facetFlags)
	if err != nil {
		return n, err
	}
	n.facets = mergeFacets(n.facets, extra)
	if presetName == "" && len(extra) > 0 && n.view == "" {
		return n, fmt.Errorf("--facet requires a view argument (e.g. dtui logs --facet loglevel=ERROR)")
	}
	if n.view != "" && catalog.Lookup(n.view) == nil && !isBespokeView(n.view) && !isNavView(n.view) {
		return n, fmt.Errorf("unknown view %q", n.view)
	}
	return n, nil
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
		if strings.ContainsAny(field, "`|\n\r") {
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

func writePresetList(w io.Writer, ws *workspace.Workspace) {
	if ws == nil || len(ws.Presets) == 0 {
		fmt.Fprintln(w, "no presets")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tVIEW\tNARROWING")
	for _, name := range ws.PresetNames() {
		p := ws.Presets[name]
		var parts []string
		for _, f := range p.Facets {
			parts = append(parts, f.Field+"="+f.Value)
		}
		for _, s := range p.Searches {
			parts = append(parts, "/"+s)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", name, p.View, strings.Join(parts, ", "))
	}
	_ = tw.Flush()
}

func namedPresets(ws *workspace.Workspace) []tui.NamedPreset {
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
		})
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

func personalPresetsPath() string {
	return filepath.Join(xdg.StateHome, "dtui", "presets.yaml")
}

func loadPersonalPresets() (*workspace.Workspace, string) {
	path := personalPresetsPath()
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
