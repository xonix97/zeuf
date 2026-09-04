import { theme, symbols } from "./theme";
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
import { visibleWidth, padRight, padCenter, truncate } from "./utils";

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
      workdir,
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
      if ((this.statusState.model === "auto" || !this.statusState.model) && ms.length > 0) {
        this.statusState.model = ms[0].id;
      }
      this.render();
    });

    // Setup terminal
    this.width = process.stdout.columns || 80;
    this.height = process.stdout.rows || 24;

    process.stdout.write("\x1b[?1049h"); // Alternate screen buffer
    process.stdout.write("\x1b[2J");     // Clear entire screen
    process.stdout.write("\x1b[?25l");   // Hide hardware cursor (we render a software block cursor)

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
    process.stdout.write("\x1b[?25h");   // Show cursor
    process.stdout.write("\x1b[?1049l"); // Restore screen
  }

  render(): void {
    const w = this.width;
    const h = this.height;

    // Determine overlays
    let overlayLines: string[] = [];
    if (this.approval.current) {
      overlayLines = this.approval.render(w);
    } else if (this.picker.active) {
      overlayLines = this.picker.render(this.router.modelsCache, this.statusState.model, w, h);
    } else if (this.input.startsWith("/")) {
      const filtered = this.popup.filter(this.input);
      overlayLines = this.popup.render(filtered, w);
    }

    const headerHeight = 3;
    const inputHeight = 3;
    const statusHeight = 1;
    const overlayHeight = overlayLines.length;

    const chatHeight = Math.max(3, h - (headerHeight + inputHeight + statusHeight + overlayHeight));

    // Render chat lines
    const chatLines = this.chatView.render(
      w,
      chatHeight,
      this.statusState.model,
      this.statusState.branch || "master"
    );

    const screenLines: string[] = [];

    // 1. Header Frame (3 lines)
    const title = ` ${symbols.brand} ZEUF ARCHITECT `;
    const ver = ` v0.5.0 `;
    const headerBorderLen = Math.max(0, w - visibleWidth(title) - visibleWidth(ver) - 4);
    screenLines.push(
      theme.borderActive("╭─") +
      theme.bold(theme.accent(title)) +
      theme.borderActive("─".repeat(headerBorderLen)) +
      theme.dim(ver) +
      theme.borderActive("─╮")
    );

    const homeDir = process.env.HOME || "";
    const displayDir = this.tools.workdir.startsWith(homeDir)
      ? "~" + this.tools.workdir.slice(homeDir.length)
      : this.tools.workdir;
    const dirStr = ` ${symbols.folder} ${truncate(displayDir, 26)} `;
    const branchStr = this.statusState.branch ? ` ${symbols.branch} ${this.statusState.branch} ` : "";
    const activeModelStr = ` ${symbols.dot} ${truncate(this.statusState.model, 32)} `;
    const headerContent = `${theme.dim(dirStr)}│${theme.orange(branchStr)}│${theme.green(activeModelStr)}`;
    screenLines.push(theme.borderActive("│") + padRight(headerContent, w - 2) + theme.borderActive("│"));
    screenLines.push(theme.borderActive("╰" + "─".repeat(w - 2) + "╯"));

    // 2. Chat Area
    for (let i = 0; i < chatHeight; i++) {
      const line = chatLines[i] || "";
      screenLines.push(padRight(line, w));
    }

    // 3. Overlays (Approval / Picker / Slash Popup)
    for (const ol of overlayLines) {
      screenLines.push(padCenter(ol, w));
    }

    // 4. Input Container (3 lines)
    const promptTitle = ` ${symbols.chat} Prompt `;
    const promptHints = ` [Enter: Send | /: Commands | ^P: Models] `;
    const inputBorderLen = Math.max(0, w - visibleWidth(promptTitle) - visibleWidth(promptHints) - 4);
    screenLines.push(
      theme.borderActive("╭─") +
      theme.bold(theme.white(promptTitle)) +
      theme.borderActive("─".repeat(inputBorderLen)) +
      theme.dim(promptHints) +
      theme.borderActive("─╮")
    );

    const cursorChar = theme.accent("█");
    const promptArrow = theme.orange(" › ");
    const inputRow = promptArrow + theme.bold(theme.white(this.input)) + cursorChar;
    screenLines.push(theme.borderActive("│") + padRight(inputRow, w - 2) + theme.borderActive("│"));
    screenLines.push(theme.borderActive("╰" + "─".repeat(w - 2) + "╯"));

    // 5. Status Bar (1 line)
    screenLines.push(renderStatusBar(this.statusState, w));

    // Clamp output to height and draw
    const finalLines = screenLines.slice(0, h);
    process.stdout.write("\x1b[H" + finalLines.map(l => l + "\x1b[K").join("\n"));
  }

  private handleKey(key: string): void {
    // 1. Approval modal handling
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

    // 2. Model picker toggle (Ctrl+P)
    if (key === "\x10") {
      if (this.picker.active) {
        this.picker.close();
      } else {
        this.picker.open();
      }
      this.render();
      return;
    }

    // 3. Model picker active interactions
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

    // 4. Slash popup active interactions
    if (this.input.startsWith("/")) {
      const filtered = this.popup.filter(this.input);
      if (key === "\x1b[A") { // Up
        this.popup.selectedIdx = Math.max(0, this.popup.selectedIdx - 1);
        this.render();
        return;
      } else if (key === "\x1b[B") { // Down
        this.popup.selectedIdx = Math.min(filtered.length - 1, this.popup.selectedIdx + 1);
        this.render();
        return;
      } else if (key === "\t" || (key === "\r" && filtered.length > 0)) {
        const sel = filtered[this.popup.selectedIdx];
        if (sel) {
          this.input = "";
          this.render();
          this.submitLine(sel.name);
          return;
        }
      } else if (key === "\x1b") { // Esc dismisses popup
        this.input = "";
        this.render();
        return;
      }
    }

    // 5. Ctrl+C to quit or cancel
    if (key === "\x03") {
      this.cleanup();
      process.exit(0);
    }

    // 6. Up / Down History Navigation (when not in popup)
    if (key === "\x1b[A") {
      if (this.history.length > 0) {
        if (this.historyIdx === -1) this.historyIdx = this.history.length - 1;
        else this.historyIdx = Math.max(0, this.historyIdx - 1);
        this.input = this.history[this.historyIdx] || "";
        this.render();
      }
      return;
    }
    if (key === "\x1b[B") {
      if (this.history.length > 0 && this.historyIdx !== -1) {
        if (this.historyIdx < this.history.length - 1) {
          this.historyIdx++;
          this.input = this.history[this.historyIdx] || "";
        } else {
          this.historyIdx = -1;
          this.input = "";
        }
        this.render();
      }
      return;
    }

    // 7. Enter to submit
    if (key === "\r") {
      const line = this.input.trim();
      if (!line) return;

      this.input = "";
      this.cursorPos = 0;
      this.history.push(line);
      this.historyIdx = -1;

      this.submitLine(line);
      return;
    }

    // 8. Backspace
    if (key === "\x7f" || key === "\b") {
      if (this.input.length > 0) {
        this.input = this.input.slice(0, -1);
      }
      this.render();
      return;
    }

    // 9. Printable characters
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
          text: "Commands: /models (switch models), /clear (clear chat), /sessions, /status, /exit. Shortcut: Ctrl+P for model switcher.",
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
      if (cmd === "/status") {
        const { branch, dirty } = await gitStatus(this.tools.workdir);
        this.chatView.append({
          type: "system",
          text: `Workspace: ${this.tools.workdir} | Branch: ${branch}${dirty ? " (dirty)" : " (clean)"} | Model: ${this.statusState.model}`,
        });
        this.render();
        return;
      }
    }

    // User message card
    this.chatView.append({ type: "user", text: line });
    this.statusState.busy = true;
    this.statusState.turnStart = Date.now();
    this.render();

    // Streaming assistant card
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
              this.chatView.append({
                type: "assistant",
                text: "",
                model: this.statusState.model,
              });
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
