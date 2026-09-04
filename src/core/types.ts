export type Role = "system" | "user" | "assistant" | "tool";

export interface ToolCall {
  id: string;
  name: string;
  arguments: string; // JSON string
}

export interface Message {
  role: Role;
  content: string;
  id?: string;
  name?: string;
  toolCalls?: ToolCall[];
}

export interface Usage {
  inputTokens: number;
  outputTokens: number;
  totalTokens?: number;
}

export interface ToolResult {
  toolCallId: string;
  name: string;
  content: string;
  isError: boolean;
}

export type Availability = "available" | "rate_limited" | "quota_exhausted" | "auth_error" | "offline" | "unknown";

export interface ModelInfo {
  id: string;
  provider: string;
  displayName: string;
  contextLength: number;
  maxOutput?: number;
  supportsTools: boolean;
  supportsStreaming: boolean;
  isFree: boolean;
  availability: Availability;
  lastError?: string;
  scores?: {
    coding: number;
    reasoning: number;
  };
}

export type StreamEventType =
  | "token"
  | "reasoning"
  | "tool_start"
  | "tool_end"
  | "usage"
  | "switch"
  | "phase"
  | "error"
  | "notice"
  | "done";

export interface StreamEvent {
  type: StreamEventType;
  text?: string;
  reasoning?: string;
  toolName?: string;
  toolArgs?: string;
  toolOk?: boolean;
  durationMs?: number;
  usage?: Usage;
  model?: string;
  switchedFrom?: string;
  switchedTo?: string;
  switchReason?: string;
  phase?: string;
  error?: string;
}

export interface ChatRequest {
  model: string;
  messages: Message[];
  tools?: ToolDefinition[];
  temperature?: number;
  maxTokens?: number;
}

export interface ChatResponse {
  content: string;
  reasoning?: string;
  toolCalls?: ToolCall[];
  usage: Usage;
}

export interface ToolDefinition {
  name: string;
  description: string;
  parameters: Record<string, any>;
}

export interface SessionSummary {
  id: string;
  task: string;
  createdAt: number;
  updatedAt: number;
  turnsCount: number;
  model: string;
}

export interface SessionData {
  id: string;
  task: string;
  createdAt: number;
  updatedAt: number;
  model: string;
  messages: Message[];
  modifiedFiles: string[];
  checkpoints: Checkpoint[];
  memory?: string[];
}

export interface Checkpoint {
  id: string;
  timestamp: number;
  message: string;
  patch: string;
}
