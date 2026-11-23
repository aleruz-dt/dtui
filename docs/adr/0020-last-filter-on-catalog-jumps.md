# ADR-0020: A catalog jump restores that view's last unscoped server filter

**Status:** Accepted · **Date:** 2026-11-23

## Context

Named presets ([ADR-0017](0017-named-view-facet-presets.md)) store the same
narrowing (facets, `| search`) only when the user names them. A hotkey or
bare `:logs` still replaced the stack with an unfiltered table, so an
investigation that hops views retyped the same filter. Recording every
navigation would make `:preset`, history restore, and `dtui send` become the
next jump's default — named presets would auto-apply in spirit. Recording
from an entity-scoped table would drop the entity on the next unscoped jump
([ADR-0007](0007-scope-honesty.md)), which `W` already refuses.

## Decision

Each catalog view remembers its **latest unscoped DQL narrowing** for this
session only:

- The payload is what `presetDraft` would save (facets + searches, no
  timeframe, no entity). API-backed and entity-scoped tables are skipped.
- It is written when the user changes server-side narrowing (`historyMarkMsg`)
  and when launch opens an unscoped table via `--facet` / `--search`.
  `:preset`, `dtui --preset`, `H`, and `dtui send` do not write the slot —
  named presets stay never auto-applied, including the CLI form.
- A bare hotkey or `:logs` jump restores it and **says so** in the status
  line (combined with a rejected pin or segments, when those also apply).
  An argument (`:logs foo`, `--preset`, `--facet`, `dtui send`) wins.
- `F` and the esc-chain that clears facets forget it. `:ctx` drops the map.

Zero configuration: no last filter still launches as today. Named presets
stay never auto-applied.

## Consequences

- `:pods` is no longer always the unfiltered list if this session already
  narrowed pods. The status line and `F` keep that honest.
- Pin (`.`) remains the cross-view carry; last-filter does not copy a
  namespace facet from logs onto workloads.
- Personal YAML and `:preset` are unchanged (ADR-0017).
