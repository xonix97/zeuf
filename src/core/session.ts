import { existsSync, readdirSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { configDir } from "./config";
import type { SessionData, SessionSummary, Message, Checkpoint } from "./types";

export function sessionsDir(): string {
  return join(configDir(), "sessions");
}

export function generateSessionId(): string {
  const ts = new Date().toISOString().replace(/[-:T]/g, "").slice(0, 14);
  const rand = Math.random().toString(36).substring(2, 6);
  return `s-${ts}-${rand}`;
}

export function listSessions(): SessionSummary[] {
  const dir = sessionsDir();
  if (!existsSync(dir)) {
    return [];
  }

  const summaries: SessionSummary[] = [];
  try {
    const files = readdirSync(dir).filter(f => f.endsWith(".json"));
    for (const file of files) {
      try {
        const full = join(dir, file);
        const data: SessionData = JSON.parse(readFileSync(full, "utf-8"));
        summaries.push({
          id: data.id || file.replace(".json", ""),
          task: data.task || "",
          createdAt: data.createdAt || 0,
          updatedAt: data.updatedAt || data.createdAt || 0,
          turnsCount: data.messages ? Math.floor(data.messages.length / 2) : 0,
          model: data.model || "",
        });
      } catch {
        // Skip corrupt files
      }
    }
  } catch {
    return [];
  }

  return summaries.sort((a, b) => b.updatedAt - a.updatedAt);
}

export interface RecentTask {
  id: string;
  title: string;
  updatedAt: number;
  timeAgo: string;
  turns: number;
  filesCount: number;
  model: string;
}

export function formatTimeAgo(timestamp: number): string {
  if (!timestamp) return "just now";
  const diffSec = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (diffSec < 60) return "just now";
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour}h ago`;
  const diffDay = Math.floor(diffHour / 24);
  return `${diffDay}d ago`;
}

export function getRecentTasks(limit: number = 4): RecentTask[] {
  const dir = sessionsDir();
  if (!existsSync(dir)) return [];

  const tasks: RecentTask[] = [];
  try {
    const files = readdirSync(dir).filter(f => f.endsWith(".json"));
    for (const file of files) {
      try {
        const full = join(dir, file);
        const data: SessionData = JSON.parse(readFileSync(full, "utf-8"));
        let title = (data.task || "").trim();
        if (!title && data.messages && data.messages.length > 0) {
          const userMsgs = data.messages.filter(m => m.role === "user");
          const substantial = userMsgs.find(m => m.content && m.content.trim().length > 6);
          const chosen = substantial || userMsgs[0];
          if (chosen && chosen.content) {
            title = chosen.content.split("\n")[0].trim();
          }
        }
        if (!title) {
          title = "Interactive session";
        }
        if (title.length > 50) {
          title = title.slice(0, 49) + "…";
        }

        const updatedAt = data.updatedAt || data.createdAt || 0;
        const turns = data.messages ? Math.floor(data.messages.length / 2) : 0;
        const filesCount = data.modifiedFiles ? data.modifiedFiles.length : 0;

        tasks.push({
          id: data.id || file.replace(".json", ""),
          title,
          updatedAt,
          timeAgo: formatTimeAgo(updatedAt),
          turns,
          filesCount,
          model: data.model ? data.model.replace(/^opencode\//, "") : "",
        });
      } catch {
        // Skip corrupt files
      }
    }
  } catch {
    return [];
  }

  return tasks.sort((a, b) => b.updatedAt - a.updatedAt).slice(0, limit);
}

export function loadSession(id: string): SessionData | null {
  const p = join(sessionsDir(), `${id}.json`);
  if (!existsSync(p)) {
    return null;
  }
  try {
    const raw = readFileSync(p, "utf-8");
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

export function saveSession(data: SessionData): void {
  try {
    const dir = sessionsDir();
    if (!existsSync(dir)) {
      mkdirSync(dir, { recursive: true, mode: 0o700 });
    }
    data.updatedAt = Date.now();
    const p = join(dir, `${data.id}.json`);
    writeFileSync(p, JSON.stringify(data, null, 2), { mode: 0o600 });
  } catch (err) {
    // Session write errors shouldn't crash the agent
  }
}

