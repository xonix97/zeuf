# Zeuf architecture

Adapted to Go conventions: one module, `internal/` packages, thin UI clients.

```text
zeuf/
├── main.go                    # entrypoint (CLI)
├── internal/
│   ├── core/                  # provider-agnostic types, session, redaction
│   │   └── tools/             # tool runtime (read/write/edit/bash/grep/glob/git/plan)
│   ├── auth/                  # credentials: OS keychain → 0600 file fallback
│   ├── providers/             # Adapter interface + health
│   │   ├── direct/            # native OpenAI-compatible HTTP (Zeuf owns loop)
│   │   ├── cligw/             # shared CLI-gateway logic (no raw API exists)
│   │   ├── opencode/          # your own `opencode` CLI / serve API
│   │   ├── kilo/              # your own `kilo` CLI / serve API
│   │   └── mock/              # scripted backend for tests
│   ├── router/                # registry, scoring, health/cooldowns, fallback
│   ├── agent/                 # orchestrator loop (plan, parallel tools, delegate subagents, approvals hub)
│   ├── config/                # user config (no secrets) + connect presets
│   ├── cli/                   # cobra CLI + REPL + /connect wizard core
│   └── tui/                   # bubbletea client (viewport, picker, wizard, approval modal)
├── docs/
└── gui/                       # FUTURE: desktop client over the same core
```

## Data flow

```text
user task
  → agent.RunTurn (orchestrator: plan → parallel tools / delegate subagents)
  → router.Ranked (filter incompatible → score → order)
  → router.Do / DoStream (try best, fallback on retryable errors)
  → adapter.Chat / Stream (native or delegated turn)
  → tools.Execute (native backends; structured, approved, truncated)
      → approvals via Hub in TUI (modal), stdin in REPL
  → session updated (history, plan, files, trail)
```

## Key invariants

- The agent runtime contains no provider-specific logic.
- Unknown scores stay unknown (`-1` → displayed "Unknown", scored neutral
  `0.5`). Quality numbers are never fabricated.
- Quota is "Unknown" unless a provider exposes it.
- Every provider failure is classified (`rate_limited`, `quota_exhausted`,
  `auth_failure`, `network_error`, `provider_overloaded`,
  `unsupported_request`) and every classified failure is fallback-eligible
  across models, bounded by `max_attempts`, cooldowns and backoff.
- Credentials never appear in logs, errors, `doctor` output or the config file.

## What's next

- Richer capability discovery (serve-API model metadata refresh).
- Per-provider cost/latency tracking feeding the scorer.
- `gui/`: desktop client (e.g. Wails) importing the same
  `internal/{core,router,agent}` packages — no duplicated agent logic.
- Plugin-style custom backends behind the `providers.Adapter` interface.
