# Security

- **No secrets in the repo or config.** Direct endpoints reference
  environment variables (`api_key_env`) or a key in the Zeuf auth store.
  The store is the OS keychain where available, else
  `~/.config/zeuf/auth.json` (0600, same practice as gh/aws CLIs).
  The opencode/kilo gateways reuse the user's own CLI logins; Zeuf never
  reads their credential files.
- **No secret output.** `core.Redact` masks bearer tokens, `sk-…`/API-key
  patterns and `KEY=…` assignments in all errors, logs and `doctor` output.
  `doctor` reports only key *presence* (`set`/`missing`), never values.
- **Models never see credentials.** Keys live only in the HTTP layer of
  the direct adapter and are never placed in prompts or tool results.
- **Approvals.** Ordinary development work — reads, in-workdir writes/edits,
  builds, tests, inspections — proceeds without prompting. Approval is
  required for out-of-workdir writes, mutating git ops, and destructive
  shell patterns (`rm -rf /`, `mkfs`, `dd … of=/dev/`, fork bombs,
  `git clean -fdx`, piped-to-shell), which always need explicit approval,
  even with `--auto` (which otherwise allows everything). Nothing
  destructive runs silently.
- **No provider circumvention.** Quotas and rate limits are respected as
  observed signals: classified failures cause cooldowns and fallback, not
  retries against the same exhausted model.
- Config file is written `0600`; tool output is size-bounded to avoid
  prompt-injection via giant outputs (still: treat tool output as data).
