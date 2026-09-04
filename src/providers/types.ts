import type { ChatRequest, ChatResponse, ModelInfo, StreamEvent } from "../core/types";

export interface Provider {
  name: string;
  isAvailable(): Promise<boolean>;
  listModels(): Promise<ModelInfo[]>;
  chat(req: ChatRequest): Promise<ChatResponse>;
  stream(req: ChatRequest, onEvent: (ev: StreamEvent) => void): Promise<ChatResponse>;
}
