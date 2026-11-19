# ADR-0019: Agents may navigate a listening TUI

**Status:** Accepted · **Date:** 2026-11-19 · Partially supersedes
[ADR-0018](0018-launch-flags-are-a-handoff-contract.md) ("agents emit
commands; they never drive dtui")

## Context

ADR-0018 lets an agent hand the user one launch command per piece of
evidence. In a longer investigation that means a new command to copy, a new
process and a lost back stack for each step (#68). The user wants to watch
the evidence appear in the TUI they already have open and drill in
themselves.

## Decision

A TUI started with `dtui --listen` accepts **page requests** on a per-user
Unix socket; `dtui send <view> [arg] [flags]` delivers one.

- The request is a `NavigateMsg`, the same descriptor the launch flags and
  deep links build. `send` takes exactly the launch grammar (ADR-0018), so
  there is still one way to name a page.
- Pages are *pushed*, so `esc` returns to what the user was reading, and the
  status line says an agent opened them.
- A remote request never switches context: the user chose the tenant this
  TUI shows.
- The socket is local (`$XDG_RUNTIME_DIR/dtui/<session>.sock`, mode
  `0600`), carries descriptors only (no tenant data, credentials or
  keystrokes), and is served by the main package; `internal/tui` opens no
  listener (`TestTUIOpensNoListener`).
- The reply names the page opened or the error, so an agent can correct a
  bad request instead of leaving the user on an error.
- Listening is opt-in per run. `send` renders nothing and needs no TTY, so it
  is exempt from the agent guard; the rendering TUI keeps it.

## Consequences

- The vision anti-goal "not an agent surface" holds: agents still read data
  through dtctl's JSON envelope and get back only a page name, never rows.
  dtui shows the human what the agent points at. Reviewers should check that
  later work (row counts in replies) keeps it that way.
- Still read-only (ADR-0011): an agent can open only what the user could
  type, and each page runs the same single query (ADR-0013).
- Facets arriving over the socket go through the same `SafeFacetField`
  check as deep links.
- The reply confirms the page opened, not that it has rows; reporting the
  row count or query error is follow-up work on #68.
- The JSON field names of `NavigateMsg` are now a wire format: renaming one
  breaks `send` against an older listening TUI.
