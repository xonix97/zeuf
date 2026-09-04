#!/usr/bin/env bun
import { Router } from "../src/providers/router";
import { ToolRegistry } from "../src/tools/registry";
import { Orchestrator } from "../src/agent/orchestrator";
import { runTUI } from "../src/tui/index";
import { listSessions, generateSessionId } from "../src/core/session";
import { theme } from "../src/tui/theme";
import type { StreamEvent } from "../src/core/types";

const VERSION = "0.5.0";

function printHelp(): void {
  console.log(`Zeuf is your own coding agent. It routes tasks across the model
backends you already have (OpenCode, Anthropic, DeepSeek, OpenAI, Ollama),
preserves the session across automatic fallbacks, and executes code
tasks with tools, approvals, and streaming.

Usage:
  zeuf [flags]
  zeuf [command]

Available Commands:
  run <task>    Run a single task non-interactively
  models        List available AI models and their health
  providers     Show provider backend reachability
  sessions      List saved sessions
  doctor        Check environment, API keys, and backends
  help          Show this help message

Flags:
      --auto         auto-approve non-destructive tool actions
      --model string pin a specific model ID
      --plain        force plain CLI output instead of full-screen TUI
  -v, --version      version for zeuf
  -h, --help         help for zeuf
`);
}

async function main(): Promise<void> {
  const args = process.argv.slice(2);

  if (args.includes("-v") || args.includes("--version")) {
    console.log(`zeuf version ${VERSION}`);
    process.exit(0);
  }

  if (args.includes("-h") || args.includes("--help") || args[0] === "help") {
    printHelp();
    process.exit(0);
  }

  const autoApprove = args.includes("--auto");
  const plain = args.includes("--plain");
  let pinnedModel: string | undefined;

  const modelIdx = args.indexOf("--model");
  if (modelIdx !== -1 && args[modelIdx + 1]) {
    pinnedModel = args[modelIdx + 1];
  }

  const cmd = args[0];

  // 1. Models list
  if (cmd === "models") {
    const router = new Router();
    process.stdout.write("zeuf: discovering models…\n\n");
    const ms = await router.allModels(true);
    if (ms.length === 0) {
      console.log("No models found. Run `opencode auth` or set API keys (ANTHROPIC_API_KEY, DEEPSEEK_API_KEY, etc).");
      process.exit(0);
    }
    console.log(theme.bold(`AVAILABLE MODELS (${ms.length})`));
    for (const m of ms) {
      const dot = m.availability === "available" ? theme.green("●") : theme.red("○");
      const badge = m.isFree ? theme.green("[free]") : theme.dim("[paid]");
      console.log(`${dot} ${theme.bold(m.displayName)} ${theme.dim(`(${m.provider}/${m.id})`)} ${badge}`);
    }
    process.exit(0);
  }

  // 2. Providers / Doctor
  if (cmd === "providers" || cmd === "doctor") {
    const router = new Router();
    console.log(theme.bold(`BACKEND PROVIDERS`));
    for (const [name, p] of router.providers.entries()) {
      const avail = await p.isAvailable();
      const dot = avail ? theme.green("●") : theme.red("○");
      console.log(`${dot} ${name.padEnd(14)}: ${avail ? theme.green("connected") : theme.dim("not configured")}`);
    }
    process.exit(0);
  }

  // 3. Sessions
  if (cmd === "sessions") {
    const list = listSessions();
    if (list.length === 0) {
      console.log("No saved sessions.");
      process.exit(0);
    }
    console.log(theme.bold(`SAVED SESSIONS (${list.length})`));
    for (const s of list) {
      const d = new Date(s.updatedAt).toLocaleString();
      console.log(`› ${theme.white(s.id)} ${theme.dim(`(${d})`)} - ${theme.dim(s.task || "empty")}`);
    }
    process.exit(0);
  }

  // 4. Headless run
  if (cmd === "run" || cmd === "exec" || plain) {
    let task = args.filter(a => !a.startsWith("-") && a !== "run" && a !== "exec").join(" ");
    if (!task) {
      console.error("Error: task description required for `zeuf run <task>`");
      process.exit(1);
    }

    const router = new Router();
    const tools = new ToolRegistry(process.cwd(), autoApprove);
    const orchestrator = new Orchestrator(router, tools);

    const session = {
      id: generateSessionId(),
      task,
      createdAt: Date.now(),
      updatedAt: Date.now(),
      model: pinnedModel || "auto",
      messages: [],
      modifiedFiles: [],
      checkpoints: [],
    };

    process.stdout.write("zeuf: discovering models…\n\n");

    try {
      const out = await orchestrator.execute(
        task,
        session,
        (ev: StreamEvent) => {
          if (ev.type === "token" && ev.text) {
            process.stdout.write(ev.text);
          } else if (ev.type === "reasoning" && ev.reasoning) {
            // reasoning tokens
          } else if (ev.type === "tool_start") {
            process.stdout.write(`\n${theme.orange("●")} ${theme.bold(ev.toolName || "tool")}: ${theme.dim(ev.toolArgs || "")}\n`);
          } else if (ev.type === "tool_end") {
            const icon = ev.toolOk ? theme.green("✓") : theme.red("✗");
            process.stdout.write(`${icon} ${theme.dim(ev.text ? ev.text.slice(0, 100) : "ok")}\n\n`);
          }
        },
        pinnedModel
      );
      process.stdout.write("\n");
      process.exit(0);
    } catch (err: any) {
      console.error(`\nError: ${err.message}`);
      process.exit(1);
    }
  }

  // 5. Interactive Full-Screen React Ink TUI
  await runTUI(process.cwd(), autoApprove);
}

main().catch(err => {
  console.error("Fatal error:", err);
  process.exit(1);
});
