import type { SessionData } from "../core/types";
import { addChatMemory, removeChatMemory, initSessionMemory } from "../core/memory";

export interface RememberArgs {
  fact: string;
}

export interface ForgetArgs {
  query: string;
}

export function rememberTool(
  session?: SessionData,
  args?: RememberArgs
): { content: string; isError: boolean } {
  if (!session) {
    return { content: "Error: No active chat session to record memory.", isError: true };
  }

  const fact = (args?.fact || "").trim();
  if (!fact) {
    return { content: "Error: 'fact' parameter is required for remember tool.", isError: true };
  }

  const { added, count } = addChatMemory(session, fact);
  if (added) {
    return {
      content: `Saved to chat memory: "${fact}" (${count} total memories in this chat)`,
      isError: false,
    };
  } else {
    return {
      content: `Chat memory already contains: "${fact}"`,
      isError: false,
    };
  }
}

export function forgetTool(
  session?: SessionData,
  args?: ForgetArgs
): { content: string; isError: boolean } {
  if (!session) {
    return { content: "Error: No active chat session to modify memory.", isError: true };
  }

  const query = (args?.query || "").trim();
  if (!query) {
    return { content: "Error: 'query' parameter is required for forget tool.", isError: true };
  }

  const removed = removeChatMemory(session, query);
  if (removed) {
    const remaining = initSessionMemory(session).length;
    return {
      content: `Removed memory matching "${query}" from this chat. (${remaining} remaining)`,
      isError: false,
    };
  } else {
    return {
      content: `No memory found matching "${query}" in this chat session.`,
      isError: false,
    };
  }
}
