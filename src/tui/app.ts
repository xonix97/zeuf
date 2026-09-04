import { theme } from "./theme";
import { ChatView } from "./components/chat";
import { CommandPopup } from "./components/popup";
import { ModelPicker } from "./components/picker";
import { ApprovalModal } from "./components/approval";
import { renderStatusBar, type StatusState } from "./components/statusbar";
import { Orchestrator } from "../agent/orchestrator";
import { Router } from "../providers/router";
import { ToolRegistry } from "../tools/registry";
import { gitStatus } from "../tools/git";
import { generateSessionId, loadSession, saveSession } from "../core/session";
import type { SessionData, StreamEvent } from "../core/types";

export class TUIApp {
  router: Router;
  tools: ToolRegistry;
  orchestrator: Orchestrator;
  session: SessionData;

  chatView: ChatView = new ChatView();
  popup: CommandPopup = new CommandPopup();
  picker: ModelPicker = new ModelPicker();
  approval: ApprovalModal = new ApprovalModal();

  input: string = "";
  cursorPos: number = 0;
  history: string[] = [];
  historyIdx: number = -1;

  statusState: StatusState;
  running: boolean = true;
  private width: number = 80;
  private height: number = 24;

  constructor(workdir: string = process.cwd(), autoApprove: boolean = false, sessionId?: string) {
    this.router = new Router();
    this.tools = new ToolRegistry(workdir, autoApprove, this.askApproval.bind(this));
    this.orchestrator = new Orchestrator(this.router, this.tools);

    const loaded = sessionId ? loadSession(sessionId) : null;
    this.session = loaded || {
      id: sessionId || generateSessionId(),
      task: "",
      createdAt: Date.now(),
      updatedAt: Date.now(),
      model: "auto",
      messages: [],
      modifiedFiles: [],
      checkpoints: [],
    };

    this.statusState = {
      model: this.session.model || "auto",
      busy: false,
      tokensIn: 0,
      tokensOut: 0,
    };
  }

  private askApproval(toolName: string, argsJSON: string): Promise<"allow" | "always" | "deny"> {
    return new Promise(resolve => {
      this.approval.current = {
        toolName,
        argsJSON,
        resolve: decision => {
          this.approval.current = null;
          this.render();
          resolve(decision);
        },
      };
      this.render();
    });
  }

  async start(): Promise<void> {
    const { branch } = await gitStatus(this.tools.workdir);
    this.statusState.branch = branch;

    // Load available models in background
    this.router.allModels().then(ms => {
      if (this.statusState.model === "auto" && ms.length > 0) {
        this.statusState.model = ms[0].id;
      }
      this.render();
    });

    // Setup terminal
    this.width = process.stdout.columns || 80;
    this.height = process.stdout.rows || 24;

    process.stdout.write("\x1b[?1049h"); // Alternate screen buffer
    process.stdout.write("\x1b[?25h");   // Ensure cursor is shown
    if (process.stdin.isTTY) {
      process.stdin.setRawMode(true);
      process.stdin.resume();
      process.stdin.setEncoding("utf-8");
      process.stdin.on("data", this.handleKey.bind(this));
    }

    process.stdout.on("resize", () => {
      this.width = process.stdout.columns || 80;
      this.height = process.stdout.rows || 24;
      this.render();
    });

    this.render();
  }

  private cleanup(): void {
    if (process.stdin.isTTY) {
      process.stdin.setRawMode(false);
      process.stdin.pause();
    }
    process.stdout.write("\x1b[?1049l"); // Restore screen
    process.stdout.write("\x1b[?25h");   // Show cursor
  }

  render(): void {
    const out: string[] = [];
    const w = this.width;
    const h = this.height;

    // Top border
    out.push(theme.border("┌─ Zeuf " + "─".repeat(Math.max(0, w - 9)) + "┐"));

    // Chat area lines
    const chatHeight = Math.max(5, h - 8);
    const chatLines = this.chatView.render(w - 2, chatHeight);
    for (let i = 0; i < chatHeight; i++) {
      const line = chatLines[i] || "";
      const padded = line + " ".repeat(Math.max(0, w - 2 - line.replace(/\x1b\[[0-9;]*m/g, "").length));
      out.push(theme.border("│") + padded + theme.border("│"));
    }

    // Modal Overlays (Approval / Picker / Popup)
    if (this.approval.current) {
      const modalLines = this.approval.render(w);
      for (const ml of modalLines) {
        out.push(ml);
      }
    } else if (this.picker.active) {
      const pickerLines = this.picker.render(this.router.modelsCache, this.statusState.model, w, h);
      for (const pl of pickerLines) {
        out.push(pl);
      }
    } else if (this.input.startsWith("/")) {
      const filtered = this.popup.filter(this.input);
      const popLines = this.popup.render(filtered, w);
      for (const pl of popLines) {
        out.push(pl);
      }
    }

    // Input divider
    out.push(theme.border("├" + "─".repeat(Math.max(0, w - 2)) + "┤"));

    // Input line
    const prompt = theme.orange("› ");
    const inputPadded = prompt + this.input + " ".repeat(Math.max(0, w - 4 - this.input.length));
    out.push(theme.border("│ ") + inputPadded + theme.border("│"));

    // Bottom border & Status bar
    out.push(theme.border("└" + "─".repeat(Math.max(0, w - 2)) + "┘"));
    out.push(renderStatusBar(this.statusState, w));

    // Paint frame
    process.stdout.write("\x1b[H" + out.join("\n"));
  }

  private handleKey(key: string): void {
    // Approval modal handling
    if (this.approval.current) {
      if (key === "y" || key === "Y") {
        this.approval.current.resolve("allow");
      } else if (key === "a" || key === "A") {
        this.approval.current.resolve("always");
      } else if (key === "n" || key === "N" || key === "\x1b") {
        this.approval.current.resolve("deny");
      }
      return;
    }

    // Model picker handling (Ctrl+P)
    if (key === "\x10") { // Ctrl+P
      if (this.picker.active) {
        this.picker.close();
      } else {
        this.picker.open();
      }
      this.render();
      return;
    }

    if (this.picker.active) {
      const filtered = this.picker.getFiltered(this.router.modelsCache);
      if (key === "\x1b" || key === "\x10") {
        this.picker.close();
      } else if (key === "\x1b[A") { // Up
        this.picker.selectedIdx = Math.max(0, this.picker.selectedIdx - 1);
      } else if (key === "\x1b[B") { // Down
        this.picker.selectedIdx = Math.min(filtered.length - 1, this.picker.selectedIdx + 1);
      } else if (key === "\r") { // Enter
        if (filtered[this.picker.selectedIdx]) {
          const sel = filtered[this.picker.selectedIdx];
          this.statusState.model = sel.id;
          this.session.model = sel.id;
          this.chatView.append({ type: "system", text: `Switched model to ${sel.displayName}` });
        }
        this.picker.close();
      } else if (key === "\x7f" || key === "\b") {
        this.picker.filterText = this.picker.filterText.slice(0, -1);
      } else if (key.length === 1 && key >= " ") {
        this.picker.filterText += key;
      }
      this.render();
      return;
    }

    // Ctrl+C to quit or cancel
    if (key === "\x03") {
      this.cleanup();
      process.exit(0);
    }

    // Enter
    if (key === "\r") {
      const line = this.input.trim();
      if (!line) return;

      this.input = "";
      this.cursorPos = 0;
      this.history.push(line);
      this.historyIdx = this.history.length;

      this.submitLine(line);
      return;
    }

    // Backspace
    if (key === "\x7f" || key === "\b") {
      if (this.input.length > 0) {
        this.input = this.input.slice(0, -1);
      }
      this.render();
      return;
    }

    // Tab autocomplete
    if (key === "\t" && this.input.startsWith("/")) {
      const filtered = this.popup.filter(this.input);
      if (filtered.length > 0) {
        this.input = filtered[0].name + " ";
      }
      this.render();
      return;
    }

    // Printable character
    if (key.length === 1 && key >= " ") {
      this.input += key;
      this.render();
    }
  }

  private async submitLine(line: string): Promise<void> {
    // Handle local slash commands
    if (line.startsWith("/")) {
      const cmd = line.trim().split(" ")[0];
      if (cmd === "/clear") {
        this.chatView.blocks = [];
        this.render();
        return;
      }
      if (cmd === "/help") {
        this.chatView.append({
          type: "system",
          text: "Commands: /models (switch models), /clear (clear chat), /status, /exit. Shortcut: Ctrl+P for model switcher.",
        });
        this.render();
        return;
      }
      if (cmd === "/exit" || cmd === "/quit") {
        this.cleanup();
        process.exit(0);
      }
      if (cmd === "/models") {
        this.picker.open();
        this.render();
        return;
      }
    }

    // User message block
    this.chatView.append({ type: "user", text: line });
    this.statusState.busy = true;
    this.statusState.turnStart = Date.now();
    this.render();

    // Streaming assistant block
    let assistantBlock: { text: string } | null = null;

    try {
      await this.orchestrator.execute(
        line,
        this.session,
        (ev: StreamEvent) => {
          if (ev.type === "reasoning" && ev.reasoning) {
            this.chatView.append({
              type: "thinking",
              text: ev.reasoning,
            });
          } else if (ev.type === "token" && ev.text) {
            if (!assistantBlock) {
              assistantBlock = { text: "" };
              this.chatView.append({ type: "assistant", text: "" });
            }
            assistantBlock.text += ev.text;
            const last = this.chatView.blocks[this.chatView.blocks.length - 1];
            if (last && last.type === "assistant") {
              last.text = assistantBlock.text;
            }
          } else if (ev.type === "tool_start") {
            this.chatView.append({
              type: "tool",
              text: "",
              toolName: ev.toolName,
              toolArgs: ev.toolArgs,
            });
          } else if (ev.type === "tool_end") {
            const last = this.chatView.blocks[this.chatView.blocks.length - 1];
            if (last && last.type === "tool") {
              last.toolOk = ev.toolOk;
              last.durationMs = ev.durationMs;
              last.text = ev.text || "";
            }
          } else if (ev.type === "usage" && ev.usage) {
            this.statusState.tokensIn += ev.usage.inputTokens;
            this.statusState.tokensOut += ev.usage.outputTokens;
          } else if (ev.type === "switch") {
            this.chatView.append({
              type: "system",
              text: `Switched from ${ev.switchedFrom} to ${ev.switchedTo} (${ev.switchReason})`,
            });
          }
          this.render();
        },
        this.statusState.model === "auto" ? undefined : this.statusState.model
      );

      saveSession(this.session);
    } catch (err: any) {
      this.chatView.append({ type: "error", text: err.message || String(err) });
    } finally {
      this.statusState.busy = false;
      this.render();
    }
  }
}
