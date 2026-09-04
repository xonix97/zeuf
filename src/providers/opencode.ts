import { spawn } from "node:child_process";
import type { Provider } from "./types";
import type { ChatRequest, ChatResponse, ModelInfo, StreamEvent, ToolCall } from "../core/types";

export class OpenCodeProvider implements Provider {
  name = "opencode";
  private bin = "opencode";

  async isAvailable(): Promise<boolean> {
    try {
      const proc = Bun.spawn(["which", "opencode"]);
      const code = await proc.exited;
      return code === 0;
    } catch {
      return false;
    }
  }

  async listModels(): Promise<ModelInfo[]> {
    try {
      const proc = Bun.spawn(["opencode", "models"], { stderr: "ignore" });
      const text = await new Response(proc.stdout).text();
      const lines = text.split("\n").map(l => l.trim()).filter(Boolean);

      return lines.map(line => {
        let id = line;
        let provider = "opencode";
        if (line.includes("/")) {
          const parts = line.split("/");
          provider = parts[0];
          id = parts.slice(1).join("/");
        }
        return {
          id: line,
          provider: "opencode",
          displayName: id.replace(/[-_]/g, " ").replace(/\b\w/g, c => c.toUpperCase()),
          contextLength: 200000,
          supportsTools: true,
          supportsStreaming: true,
          isFree: true,
          availability: "available",
          scores: {
            coding: 88,
            reasoning: 85,
          },
        };
      });
    } catch {
      return [
        {
          id: "opencode/big-pickle",
          provider: "opencode",
          displayName: "Big Pickle",
          contextLength: 200000,
          supportsTools: true,
          supportsStreaming: true,
          isFree: true,
          availability: "available",
        },
      ];
    }
  }

  async chat(req: ChatRequest): Promise<ChatResponse> {
    const prompt = this.formatMessages(req);
    const model = req.model.includes("/") ? req.model : `opencode/${req.model}`;

    return new Promise((resolve, reject) => {
      const child = spawn("opencode", ["run", "--pure", "--format", "json", "--model", model, prompt], {
        env: { ...process.env, CI: "true" },
        stdio: ["ignore", "pipe", "pipe"],
      });

      let stdout = "";
      let stderr = "";

      child.stdout.on("data", d => { stdout += d.toString(); });
      child.stderr.on("data", d => { stderr += d.toString(); });

      child.on("close", code => {
        if (code !== 0 && !stdout.trim()) {
          return reject(new Error(`opencode exited with code ${code}: ${stderr || stdout}`));
        }

        let fullText = "";
        let inputTokens = Math.round(prompt.length / 4);
        let outputTokens = 0;

        const lines = stdout.split("\n").filter(Boolean);
        for (const line of lines) {
          try {
            const ev = JSON.parse(line);
            if (ev.type === "text" && ev.part?.text) {
              fullText += ev.part.text;
            }
            if (ev.type === "step_finish" && ev.part?.tokens) {
              if (ev.part.tokens.input) inputTokens = ev.part.tokens.input;
              if (ev.part.tokens.output) outputTokens = ev.part.tokens.output;
            }
          } catch {
            // Fallback for non-JSON lines
            if (!line.startsWith("> ") && !line.startsWith("{")) {
              fullText += (fullText ? "\n" : "") + line;
            }
          }
        }

        resolve({
          content: fullText.trim(),
          usage: {
            inputTokens,
            outputTokens: outputTokens || Math.round(fullText.length / 4),
          },
        });
      });

      child.on("error", reject);
    });
  }

  async stream(req: ChatRequest, onEvent: (ev: StreamEvent) => void): Promise<ChatResponse> {
    const prompt = this.formatMessages(req);
    const model = req.model.includes("/") ? req.model : `opencode/${req.model}`;

    return new Promise((resolve, reject) => {
      const child = spawn("opencode", ["run", "--pure", "--format", "json", "--model", model, prompt], {
        env: { ...process.env, CI: "true" },
        stdio: ["ignore", "pipe", "pipe"],
      });

      let fullText = "";
      let buffer = "";
      let inputTokens = Math.round(prompt.length / 4);
      let outputTokens = 0;

      child.stdout.on("data", d => {
        buffer += d.toString();
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";

        for (const line of lines) {
          if (!line.trim()) continue;
          try {
            const ev = JSON.parse(line);
            if (ev.type === "text" && ev.part?.text) {
              fullText += ev.part.text;
              onEvent({
                type: "token",
                text: ev.part.text,
                model,
              });
            } else if (ev.type === "reasoning" && ev.part?.text) {
              onEvent({
                type: "reasoning",
                reasoning: ev.part.text,
                model,
              });
            } else if (ev.type === "step_finish" && ev.part?.tokens) {
              if (ev.part.tokens.input) inputTokens = ev.part.tokens.input;
              if (ev.part.tokens.output) outputTokens = ev.part.tokens.output;
            }
          } catch {
            // Non-JSON line
            if (!line.startsWith("> ")) {
              fullText += line;
              onEvent({
                type: "token",
                text: line,
                model,
              });
            }
          }
        }
      });

      child.stderr.on("data", () => {});

      child.on("close", code => {
        if (buffer.trim()) {
          try {
            const ev = JSON.parse(buffer);
            if (ev.type === "text" && ev.part?.text) {
              fullText += ev.part.text;
              onEvent({ type: "token", text: ev.part.text, model });
            }
          } catch {
            if (!buffer.startsWith("> ")) {
              fullText += buffer;
              onEvent({ type: "token", text: buffer, model });
            }
          }
        }

        const usage = {
          inputTokens,
          outputTokens: outputTokens || Math.round(fullText.length / 4),
        };
        onEvent({ type: "usage", usage, model });
        onEvent({ type: "done", model });

        resolve({
          content: fullText.trim(),
          usage,
        });
      });

      child.on("error", reject);
    });
  }

  private formatMessages(req: ChatRequest): string {
    const parts: string[] = [];

    // 1. System prompt & persistent memory
    const systemMsgs = req.messages.filter(m => m.role === "system");
    if (systemMsgs.length > 0) {
      parts.push(systemMsgs.map(m => m.content).join("\n\n"));
    }

    // 2. Tools documentation if provided
    if (req.tools && req.tools.length > 0) {
      const toolDocs = req.tools.map(t => `- ${t.name}: ${t.description}`).join("\n");
      parts.push(`AVAILABLE TOOLS:\n${toolDocs}`);
    }

    // 3. Conversation turns
    const convMsgs = req.messages.filter(m => m.role !== "system");
    if (convMsgs.length > 0) {
      const formatted = convMsgs.map(m => {
        if (m.role === "user") return `User: ${m.content}`;
        if (m.role === "assistant") return `Assistant: ${m.content}`;
        if (m.role === "tool") return `Tool Result (${m.name}): ${m.content}`;
        return `${m.role}: ${m.content}`;
      }).join("\n\n");
      parts.push(formatted);
    }

    return parts.join("\n\n---\n\n");
  }
}
