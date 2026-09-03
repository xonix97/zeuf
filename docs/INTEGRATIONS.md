# Integration mechanisms (verified, 2026-09-03)

Before implementation we inspected what OpenCode (`1.18.23`) and Kilo
Code (`7.5.6`) can actually expose. Both are the same lineage (headless
`serve` + `run` CLI), and both gate their hosted models behind the
user's own login. There is **no documented raw-completions API** for
their gateway models — the supported programmatic surfaces are:

## Verified surfaces (all local, all using the user's own credentials)

| Mechanism | OpenCode | Kilo | Used for |
|-----------|----------|------|----------|
| `<bin> models [--verbose]` | ✅ plain + full JSON per model (id, limits, `toolcall`) | ✅ same shape | discovery (`cligw.ParseVerbose`) |
| `<bin> providers list` / `<bin> auth list` | ✅ | ✅ | health without spending quota |
| `<bin> run --format json -m <model> <prompt>` | ✅ JSONL (`step_start`, `text`, `step_finish`, `error`) | ✅ same envelope | one delegated turn per Zeuf turn |
| `<bin> serve` HTTP API (`/provider`, `/config`, `/session`, …) | ✅ probed live | ✅ same routes | optional richer discovery |

Verified live: `opencode run --format json -m opencode/mimo-v2.5-free`
returned streamed text events; `models --verbose` returned per-model
JSON with `limit.context` and `capabilities.toolcall`; unknown models
yield `{"type":"error",…}` events.

Gemini CLI (0.58.0, verified live): no model enumeration, so Zeuf ships
the documented IDs (2.5 pro/flash with public 1M windows; 3 previews as
context-unknown rather than guessed). Headless turns use the documented
envelopes — `-o json` (`{response, stats, error?}`) and `-o stream-json`
(JSONL `init`/`message`/`tool_use`/`tool_result`/`error`/`result`) —
with `--thinking` for reasoning parts and step-finish token accounting
folded into usage. Verified: unauthenticated runs return error code 41
(mapped to auth failure); `reasoning` parts, `tool_use` completions and
per-step `tokens` all parse. Auth = your `gemini` login or
`GEMINI_API_KEY` (presence only — Zeuf never reads credential files).
Note: Google end-of-lifed CLI OAuth for individuals (IneligibleTier —
use a free AI Studio key instead); Zeuf classifies that wall as an auth
failure so routing skips the backend while healthy ones exist, and a
failed backend's sibling models are skipped within a turn since new
credentials won't appear mid-turn.

Gemini model IDs follow the official selection docs and API changelog:
stable 2.5 pro/flash/flash-lite, 2.0-flash, 3.5/3.6-flash (+3.5-lite),
3.1-pro and the 3/3-flash previews. Context is stated only where publicly
documented (the 1M generations); scores and quota stay unknown. The
flash/flash-lite family plus 2.0-flash are marked free — they are
eligible for Google's $0 tier (CLI login / AI Studio free quota) — while
pro/preview and ambiguous IDs stay unmarked, since paid keys and Vertex
still meter. Paid-only releases (e.g. 3.7/3.8 Flash) are deliberately
excluded from the free set.

## Consequences for the design

- **Direct providers** (`direct/*`): Zeuf owns the complete loop —
  OpenAI-compatible chat + tools + SSE streaming over HTTPS.
- **Gateway backends** (`opencode`, `kilo`, `gemini`): Zeuf delegates each turn to
  the user's own CLI/server (the integration these tools explicitly
  support) and keeps everything else: agent state, transcript
  reconstruction, routing, fallback, approvals policy, UI. The full
  history (system prompt, plan, files inspected, tool results) is folded
  into every delegated turn, so a model switch continues the same task.
  Every delegated prompt also carries an action directive (act in the
  workdir with tools, verify, summarize briefly) so gateway agents
  implement tasks instead of dictating code.
- CLI gateways that accept stdin prompts (`opencode run`, `kilo run`)
  receive the transcript on stdin, never argv: prompts carry session
  content (visible to local users via `ps`) and can exceed ARG_MAX.
  Backends without a stdin prompt mode (`gemini -p` appends stdin to the
  flag; `agy` only reads prompts via stream-json sessions) keep argv
  invocation, documented per adapter.
- Adding a provider later means implementing `providers.Adapter`
  (`ListModels`, `Chat`, `Stream`, `Health`) — the agent never changes.

## What Zeuf will not do

- No reading of credential files, no token replay outside the user's own
  tools, no scraping, no private-endpoint reverse engineering, no quota
  bypass. Rate-limit/quota errors propagate as classified failures and
  trigger legitimate fallback to another backend.
