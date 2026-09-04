import { existsSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { homedir } from "node:os";

export type RoutingMode = "auto" | "balanced" | "fastest" | "quality";

export interface EndpointConfig {
  name: string;
  baseUrl: string;
  apiKey?: string;
  models?: string[];
}

export interface ZeufConfig {
  mode: RoutingMode;
  pinnedModel?: string;
  autoApprove: boolean;
  preferredProviders: string[];
  endpoints: EndpointConfig[];
  timeoutMs: number;
}

export const defaultConfig: ZeufConfig = {
  mode: "auto",
  autoApprove: false,
  preferredProviders: ["opencode", "anthropic", "deepseek", "kilo"],
  endpoints: [],
  timeoutMs: 120000,
};

export function configDir(): string {
  const home = homedir();
  return join(home, ".zeuf");
}

export function configPath(): string {
  return join(configDir(), "config.json");
}

export function loadConfig(): ZeufConfig {
  try {
    const p = configPath();
    if (existsSync(p)) {
      const raw = readFileSync(p, "utf-8");
      const parsed = JSON.parse(raw);
      return { ...defaultConfig, ...parsed };
    }
  } catch (err) {
    // Return fallback
  }
  return { ...defaultConfig };
}

export function saveConfig(cfg: ZeufConfig): void {
  try {
    const dir = configDir();
    if (!existsSync(dir)) {
      mkdirSync(dir, { recursive: true, mode: 0o700 });
    }
    writeFileSync(configPath(), JSON.stringify(cfg, null, 2), { mode: 0o600 });
  } catch (err) {
    console.error("Failed to save config:", err);
  }
}
