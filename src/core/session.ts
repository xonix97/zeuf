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
