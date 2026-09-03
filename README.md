# Zeuf — your own coding agent

One coding agent, many model sources. Zeuf routes tasks across the model
backends you already have, preserves the session across automatic
fallbacks, and does real coding work with tools, approvals and streaming.

```text
                         ZEUF
                Coding Agent / TUI / CLI
                           │
                     Model Router
                           │
          ┌────────────────┼────────────────┐
          │                │                │
      OpenCode          Kilo Code       Direct APIs
      backend            backend        / providers
          │                │                │
       Models            Models           Models
```

You interact only with Zeuf. External ecosystems are model
infrastructure behind Zeuf's model layer — never separate agents you
have to drive yourself.

## Quick start

```bash
git clone https://github.com/xonix97/zeuf && cd zeuf
./install.sh   # Go + opencode + kilo + gemini CLIs (skips what's present), builds zeuf
```

Requirements (all auto-installed by `./install.sh` when missing, except
Go and Node which it points you to): Go ≥ 1.24 to build zeuf, Node ≥ 20
for the Gemini CLI, and the model CLIs themselves — OpenCode, Kilo Code
and Gemini CLI — which zeuf uses as model backends. Then authenticate
whichever backends you want models from (`opencode auth login`,
`kilo auth login`, `gemini`, or `zeuf connect` for API keys).

```bash
./zeuf init
./zeuf doctor
./zeuf models          # free models only (--all includes paid/unknown-cost)
./zeuf models --all
./zeuf connect         # attach OpenRouter / Ollama / custom / CLI logins
./zeuf                                    # interactive session
./zeuf run --auto "fix the failing test"  # one-shot task
./zeuf tui                                # full-screen TUI
```

TUI keys: `enter` send · `ctrl+j` newline · `↑/↓` history · `pgup/pgdn`
scroll · `ctrl+p` model picker · `?` help · `ctrl+c` quit. Sensitive
tool actions pop an approval modal (`y`/`n`) — nothing destructive runs
silently, including in the TUI.

The TUI is an agent surface, not a chat log: session header (cwd, git
branch, dirty flag), live tool steps with spinner → `✓`/`✗` + elapsed
time and result previews, an in-place plan checklist, edit diffstats
(`+8 -3`), and full markdown rendering — fenced/inline code with syntax
highlighting plus styled LaTeX spans (`$…$`, `$$…$$`). Each assistant
message renders exactly once: streamed tokens fold into the final text
instead of echoing it.

Interactive commands: `/models [all]` (fuzzy picker, enter pins),
`/connect` (attach a backend without leaving Zeuf),
`/router auto|balanced|fastest|quality|pin <id>|unpin|fallback|nofallback`,
`/providers`, `/session`, `/quit`.

## Orchestrator

Zeuf works as an agentic orchestrator: it decomposes tasks into a
tracked plan, issues independent tool calls in parallel, and delegates
parallelizable subtasks to depth-capped subagents whose summaries fold
back into the parent plan. Failed models fall back mid-task with the
full session preserved.

## Backends

| Backend  | How it connects | Agent loop |
|----------|-----------------|------------|
| `opencode` | Your own `opencode` CLI (`models --verbose`, `run --format json`) + optional `opencode serve` API for discovery | Delegated turn per Zeuf turn (gateway exposes no raw model API); Zeuf keeps session/routing/fallback/UI |
| `kilo` | Your own `kilo` CLI, same pattern | Delegated, same contract |
| `gemini` | Your own `gemini` CLI headless mode (`-p … -o json` / `-o stream-json`, `--yolo`) | Delegated, same contract; thinking blocks + usage parsed from the documented JSON envelopes |
| `direct:*` | OpenAI-compatible HTTPS (`/chat/completions`, SSE), key from env/store, or keyless for local servers | Native — Zeuf owns the full loop incl. tools |
| `mock` | In-process scripted backend | Tests only |

`zeuf connect` attaches backends without leaving the app: OpenRouter
(includes free models), Google Gemini (free-tier AI Studio key),
OpenAI/Anthropic with your own keys (BYOK), local Ollama/LM Studio
models (free, no key — loopback endpoints are metered by nothing, so
they list as Free), or any custom OpenAI-compatible endpoint. CLI
logins (opencode/kilo) reuse those tools' own auth.

Details: `docs/INTEGRATIONS.md`. Security: `docs/SECURITY.md`.
Architecture: `docs/ARCHITECTURE.md`.

## Configuration

`~/.config/zeuf/config.json` (created by `zeuf init`). Secrets are never
stored there — direct endpoints reference environment variables:

```json
{
  "backends_order": ["opencode", "kilo", "direct"],
  "direct": [
    {"name": "openrouter", "base_url": "https://openrouter.ai/api/v1", "api_key_env": "OPENROUTER_API_KEY"}
  ],
  "prefs": {"mode": "auto", "fallback_enabled": true, "max_attempts": 4},
  "auto_approve": false
}
```

`api_key_env` empty means "key in the Zeuf auth store". Secrets live in
the OS keychain when available, else `~/.config/zeuf/auth.json` (0600) —
check `zeuf doctor` for per-endpoint credential state (names only).

## Status

Milestone 1 (working): interactive session → real provider → tool
calls → file edits → streaming → failure → automatic fallback with the
same session continuing. See `docs/ARCHITECTURE.md` for what's next
(desktop GUI shares `internal/` core; TUI/CLI/GUI are thin clients).
