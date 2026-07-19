# Multi-context: activation + org-spanning views

Status: approved design (2026-07-18)

## Goal

Let the user activate more than one context (org) and have views span all
active orgs, each row tagged with its origin context. k9s is structurally
single-cluster-per-view; this is ike's clearest differentiator.

## Model

- **Activation is opt-in per context, in `:ctx`.** `space` toggles a context
  active. Exactly one active stays the default; the *current* context is
  always active (deactivating it is rejected with a flash).
- **Current context keeps its meaning.** `enter` in `:ctx` still switches the
  current context; server-query prompts, `:settings`, saved queries and
  session restore keep working against the current context exactly as today.
- **Persistence:** `active: true` on the context in the config file, written
  through the existing config-callback pattern. Session restore then brings
  back the whole active set.

## Which views span (staged)

**Stage 1 — cheap client-filtered lists:** monitors, incidents, SLOs,
downtimes. These are the on-call-relevant lists and the two auto-refresh
views. Fetches run in parallel across active contexts (each org spends only
its own rate-limit budget), results merge, and each view's natural order is
kept (e.g. monitors still sort Alert-first).

**Stage 2 (separate spec):** evaluate spanning for the server-query views
(logs, traces, events), services and dashboards; and a possible `:overview`
screen (open incidents + alerting monitors together, cross-resource triage).
Decide after living with stage 1.

## UI

- **`CTX` column, first, only when >1 context is active.** With a single
  active context the UI is pixel-identical to today (no column, no overhead).
- `:ctx` view gains an `ACTIVE` marker column and the `space` hint.
- Header: the `Budget` block shows one line per active org, prefixed with the
  context name; `Mode` keeps showing the current context.
- Errors: if one org's fetch fails, the view renders the successful orgs'
  rows and flashes the failing org's error (stale-on-error still applies per
  org). The screen never blanks because one org is down.

## Architecture

Today `App` holds one `*data.Cached`. It becomes:

- `providers map[string]*data.Cached` — one per **active** context, created
  via the existing factory on activation, dropped on deactivation (hard
  teardown per org, same boundary semantics as today's switch).
- `a.provider` stays as the current context's entry (all non-spanning paths
  are untouched).
- **Fetch (spanning views):** fan out `Fetch` over active providers in
  parallel (goroutines + `errgroup`-style collection), merge rows, tag each
  with its context.
- **`Row.Ctx string`** — origin context. Every detail fetch, drill-down and
  write routes through `providers[row.Ctx]`, falling back to the current
  provider when empty (single-context mode). This is the load-bearing change:
  a mute/incident-write on a prod row must hit prod, whatever the current
  context is.
- Switching the current context between two *active* contexts no longer
  tears down caches (they are per-org); activating/deactivating does.

## Out of scope (stage 1)

- Spanning for logs/traces/events/services/dashboards.
- `:overview`.
- Any change to write confirmation UX (confirm modals already name the row).

## Testing

- Unit: config round-trip of `active`; merge keeps per-view natural order;
  `Row.Ctx` routing picks the right provider (fake providers).
- e2e (demo mode ships demo-dev + demo-prod): activate the second context via
  `space` in `:ctx`; monitors/incidents show the `CTX` column with rows from
  both orgs; deactivate → column disappears and rows drop to one org; a write
  (incident state) on a tagged row routes to its org's provider; relaunch
  restores the active set.
