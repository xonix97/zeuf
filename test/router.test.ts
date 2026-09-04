import { describe, it, expect } from "bun:test";
import { Router } from "../src/providers/router";
import type { ModelInfo, Provider, ChatRequest, ChatResponse, StreamEvent } from "../src/core/types";

class MockProvider implements Provider {
  name: string;
  models: ModelInfo[];
  shouldFail: boolean;

  constructor(name: string, models: ModelInfo[], shouldFail = false) {
    this.name = name;
    this.models = models;
    this.shouldFail = shouldFail;
  }

  async isAvailable(): Promise<boolean> {
    return true;
  }

  async listModels(): Promise<ModelInfo[]> {
    return this.models;
  }

  async chat(req: ChatRequest): Promise<ChatResponse> {
    if (this.shouldFail) throw new Error("Mock failure");
    return { content: "Mock response from " + this.name, model: req.model };
  }

  async stream(req: ChatRequest, onEvent: (ev: StreamEvent) => void): Promise<ChatResponse> {
    if (this.shouldFail) throw new Error("Mock stream failure");
    onEvent({ type: "token", text: "Mock token" });
    return { content: "Mock response from " + this.name, model: req.model };
  }
}

describe("Zeuf Model Router", () => {
  it("ranks free models higher in auto mode", () => {
    const router = new Router();
    const mockModels: ModelInfo[] = [
      {
        id: "paid-claude",
        provider: "anthropic",
        name: "Claude 3.5 Sonnet",
        contextWindow: 200000,
        availability: "available",
        isFree: false,
        scores: { coding: 95, reasoning: 95 },
      },
      {
        id: "free-model",
        provider: "opencode",
        name: "Big Pickle",
        contextWindow: 128000,
        availability: "available",
        isFree: true,
        scores: { coding: 85, reasoning: 85 },
      },
    ];

    const ranked = router.rankModels(mockModels, "auto");
    expect(ranked[0].id).toBe("free-model");
    expect(ranked[1].id).toBe("paid-claude");
  });

  it("prioritizes pinned model when requested", () => {
    const router = new Router();
    const mockModels: ModelInfo[] = [
      {
        id: "model-a",
        provider: "opencode",
        name: "Model A",
        contextWindow: 32000,
        availability: "available",
        isFree: true,
      },
      {
        id: "pinned-special",
        provider: "anthropic",
        name: "Pinned Model",
        contextWindow: 200000,
        availability: "available",
        isFree: false,
      },
    ];

    const ranked = router.rankModels(mockModels, "auto", "pinned-special");
    expect(ranked[0].id).toBe("pinned-special");
  });

  it("falls back to secondary provider if primary fails", async () => {
    const router = new Router();
    router.providers.clear(); // remove default providers

    const failingProvider = new MockProvider(
      "failing",
      [
        {
          id: "fail-1",
          provider: "failing",
          name: "Fail Model",
          contextWindow: 8000,
          availability: "available",
          isFree: true,
        },
      ],
      true
    );

    const workingProvider = new MockProvider(
      "working",
      [
        {
          id: "work-1",
          provider: "working",
          name: "Working Model",
          contextWindow: 8000,
          availability: "available",
          isFree: false,
        },
      ],
      false
    );

    router.register(failingProvider);
    router.register(workingProvider);

    const events: StreamEvent[] = [];
    const result = await router.executeWithFallback(
      { model: "auto", messages: [{ role: "user", content: "hi" }] },
      "auto",
      undefined,
      (ev) => events.push(ev)
    );

    expect(result.model).toContain("working/work-1");
    expect(result.response.content).toContain("Mock response from working");

    // Verification of fallback switch event
    const switchEvents = events.filter((e) => e.type === "switch");
    expect(switchEvents.length).toBe(1);
    expect(switchEvents[0].switchedTo).toContain("working/work-1");
  });
});
