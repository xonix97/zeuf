import React, { useState, useEffect } from "react";
import { Box, useInput, useApp } from "ink";
import { Header } from "./components/Header";
import { HeroBanner } from "./components/HeroBanner";
import { MessageCard } from "./components/MessageCard";
import { ThinkingCard } from "./components/ThinkingCard";
import { ToolCard } from "./components/ToolCard";
import { PromptInput } from "./components/PromptInput";
import { CommandPalette, type CommandItem } from "./components/CommandPalette";
import { ModelPickerModal } from "./components/ModelPickerModal";
import { ApprovalPrompt } from "./components/ApprovalPrompt";
import { StatusBar } from "./components/StatusBar";

import { Orchestrator } from "../agent/orchestrator";
import { Router } from "../providers/router";
import { ToolRegistry } from "../tools/registry";
import { gitStatus } from "../tools/git";
import { generateSessionId, loadSession, saveSession } from "../core/session";
import type { SessionData, StreamEvent, ModelInfo } from "../core/types";

export interface AppBlock {
  id: string;
  type: "user" | "assistant" | "thinking" | "tool" | "system" | "error";
  text: string;
  toolName?: string;
  toolArgs?: string;
  toolOk?: boolean;
  durationMs?: number;
  thinkDuration?: number;
  timestamp?: string;
  model?: string;
}

export interface AppProps {
  workdir?: string;
  autoApprove?: boolean;
  sessionId?: string;
}

export const App: React.FC<AppProps> = ({
  workdir = process.cwd(),
  autoApprove = false,
  sessionId,
}) => {
  const { exit } = useApp();

  const [router] = useState(() => new Router());
  const [tools] = useState(() => new ToolRegistry(workdir, autoApprove, askApproval));
  const [orchestrator] = useState(() => new Orchestrator(router, tools));
  const [session, setSession] = useState<SessionData>(() => {
    const loaded = sessionId ? loadSession(sessionId) : null;
    return loaded || {
      id: sessionId || generateSessionId(),
      task: "",
      createdAt: Date.now(),
      updatedAt: Date.now(),
      model: "auto",
      messages: [],
      modifiedFiles: [],
      checkpoints: [],
    };
  });

  const [blocks, setBlocks] = useState<AppBlock[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [activeModel, setActiveModel] = useState(session.model || "auto");
  const [availableModels, setAvailableModels] = useState<ModelInfo[]>([]);
  const [branch, setBranch] = useState("master");
  const [tokens, setTokens] = useState(0);

  const [showModelPicker, setShowModelPicker] = useState(false);
  const [showCommandPalette, setShowCommandPalette] = useState(false);
  const [approvalRequest, setApprovalRequest] = useState<{
    toolName: string;
    argsJSON: string;
    resolve: (decision: "allow" | "always" | "deny") => void;
  } | null>(null);

  function askApproval(toolName: string, argsJSON: string): Promise<"allow" | "always" | "deny"> {
    return new Promise(resolve => {
      setApprovalRequest({
        toolName,
        argsJSON,
        resolve: decision => {
          setApprovalRequest(null);
          resolve(decision);
        },
      });
    });
  }

  // Load git branch and models on mount
  useEffect(() => {
    gitStatus(workdir).then(({ branch: b }) => {
      if (b) setBranch(b);
    });

    router.allModels().then(ms => {
      setAvailableModels(ms);
      if ((activeModel === "auto" || !activeModel) && ms.length > 0) {
        setActiveModel(ms[0].id);
      }
    });
  }, []);

  // Global key bindings
  useInput((inputStr, key) => {
    // Toggle Ctrl+P model picker
    if (key.ctrl && inputStr === "p") {
      setShowModelPicker(prev => !prev);
      setShowCommandPalette(false);
      return;
    }

    // Escape closes overlays
    if (key.escape) {
      setShowModelPicker(false);
      setShowCommandPalette(false);
      if (input.startsWith("/")) {
        setInput("");
      }
      return;
    }

    // Ctrl+C exits cleanly
    if (key.ctrl && inputStr === "c") {
      exit();
      return;
    }
  });

  const handleInputChange = (val: string) => {
    setInput(val);
    if (val.startsWith("/") && !showCommandPalette) {
      setShowCommandPalette(true);
    } else if (!val.startsWith("/") && showCommandPalette) {
      setShowCommandPalette(false);
    }
  };

  const handleCommandSelect = (item: CommandItem) => {
    setShowCommandPalette(false);
    setInput("");

    if (item.value === "/clear") {
      setBlocks([]);
      return;
    }
    if (item.value === "/models") {
      setShowModelPicker(true);
      return;
    }
    if (item.value === "/help") {
      setBlocks(prev => [
        ...prev,
        {
          id: String(Date.now()),
          type: "system",
          text: "Commands: /models, /clear, /sessions, /status, /exit. Keybindings: Ctrl+P (models), Ctrl+C (quit), Esc (dismiss).",
        },
      ]);
      return;
    }
    if (item.value === "/status") {
      gitStatus(workdir).then(({ branch: b, dirty }) => {
        setBlocks(prev => [
          ...prev,
          {
            id: String(Date.now()),
            type: "system",
            text: `Workspace: ${workdir} | Branch: ${b}${dirty ? " (dirty)" : " (clean)"} | Active: ${activeModel}`,
          },
        ]);
      });
      return;
    }
    if (item.value === "/exit") {
      exit();
      return;
    }

    submitPrompt(item.value);
  };

  const handleModelSelect = (modelId: string) => {
    setShowModelPicker(false);
    setActiveModel(modelId);
    setSession(prev => ({ ...prev, model: modelId }));
    setBlocks(prev => [
      ...prev,
      {
        id: String(Date.now()),
        type: "system",
        text: `Switched active AI model to ${modelId}`,
      },
    ]);
  };

  const submitPrompt = async (promptText: string) => {
    const text = promptText.trim();
    if (!text || busy) return;

    setInput("");
    setShowCommandPalette(false);
    setShowModelPicker(false);

    // Add user message block
    const userBlockId = String(Date.now());
    const nowTime = new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    setBlocks(prev => [
      ...prev,
      {
        id: userBlockId,
        type: "user",
        text,
        timestamp: nowTime,
      },
    ]);

    setBusy(true);

    let assistantBlockId = "";

    try {
      await orchestrator.execute(
        text,
        session,
        (ev: StreamEvent) => {
          if (ev.type === "reasoning" && ev.reasoning) {
            setBlocks(prev => [
              ...prev,
              {
                id: "think-" + Date.now(),
                type: "thinking",
                text: ev.reasoning,
              },
            ]);
          } else if (ev.type === "token" && ev.text) {
            setBlocks(prev => {
              if (!assistantBlockId) {
                assistantBlockId = "asst-" + Date.now();
                return [
                  ...prev,
                  {
                    id: assistantBlockId,
                    type: "assistant",
                    text: ev.text || "",
                    model: activeModel,
                    timestamp: new Date().toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
                  },
                ];
              }
              return prev.map(b => (b.id === assistantBlockId ? { ...b, text: b.text + ev.text } : b));
            });
          } else if (ev.type === "tool_start") {
            const toolId = "tool-" + Date.now();
            setBlocks(prev => [
              ...prev,
              {
                id: toolId,
                type: "tool",
                text: "",
                toolName: ev.toolName,
                toolArgs: ev.toolArgs,
              },
            ]);
          } else if (ev.type === "tool_end") {
            setBlocks(prev => {
              const lastTool = [...prev].reverse().find(b => b.type === "tool");
              if (!lastTool) return prev;
              return prev.map(b =>
                b.id === lastTool.id
                  ? {
                      ...b,
                      toolOk: ev.toolOk,
                      durationMs: ev.durationMs,
                      output: ev.text,
                    }
                  : b
              );
            });
          } else if (ev.type === "usage" && ev.usage) {
            setTokens(prev => prev + (ev.usage?.inputTokens || 0) + (ev.usage?.outputTokens || 0));
          } else if (ev.type === "switch") {
            setBlocks(prev => [
              ...prev,
              {
                id: "switch-" + Date.now(),
                type: "system",
                text: `Switched from ${ev.switchedFrom} to ${ev.switchedTo} (${ev.switchReason})`,
              },
            ]);
          }
        },
        activeModel === "auto" ? undefined : activeModel
      );

      saveSession(session);
    } catch (err: any) {
      setBlocks(prev => [
        ...prev,
        {
          id: "err-" + Date.now(),
          type: "error",
          text: err.message || String(err),
        },
      ]);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Box flexDirection="column" paddingX={1} width="100%">
      {/* 1. Header Frame */}
      <Header workdir={workdir} branch={branch} model={activeModel} />

      {/* 2. Main Conversation Area / Hero */}
      <Box flexDirection="column" marginY={1}>
        {blocks.length === 0 ? (
          <HeroBanner model={activeModel} branch={branch} />
        ) : (
          blocks.map(b => {
            if (b.type === "thinking") {
              return <ThinkingCard key={b.id} duration={b.thinkDuration} text={b.text} />;
            }
            if (b.type === "tool") {
              return (
                <ToolCard
                  key={b.id}
                  toolName={b.toolName || "tool"}
                  args={b.toolArgs}
                  toolOk={b.toolOk}
                  durationMs={b.durationMs}
                  output={b.text}
                />
              );
            }
            return (
              <MessageCard
                key={b.id}
                role={b.type as any}
                text={b.text}
                model={b.model}
                timestamp={b.timestamp}
              />
            );
          })
        )}
      </Box>

      {/* 3. Interactive Modals */}
      {approvalRequest && (
        <ApprovalPrompt
          toolName={approvalRequest.toolName}
          argsJSON={approvalRequest.argsJSON}
          onDecision={approvalRequest.resolve}
        />
      )}

      {showCommandPalette && (
        <CommandPalette filter={input} onSelect={handleCommandSelect} />
      )}

      {showModelPicker && (
        <ModelPickerModal
          models={availableModels}
          currentModel={activeModel}
          onSelect={handleModelSelect}
        />
      )}

      {/* 4. Input Container */}
      {!approvalRequest && !showModelPicker && (
        <PromptInput
          value={input}
          onChange={handleInputChange}
          onSubmit={submitPrompt}
          disabled={busy}
        />
      )}

      {/* 5. Status Bar */}
      <StatusBar
        model={activeModel}
        busy={busy}
        branch={branch}
        tokens={tokens}
      />
    </Box>
  );
};
