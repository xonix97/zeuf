import type { SessionData } from "./types";

/**
 * Ensure session.memory is initialized as an array
 */
export function initSessionMemory(session: SessionData): string[] {
  if (!session.memory || !Array.isArray(session.memory)) {
    session.memory = [];
  }
  return session.memory;
}

/**
 * Add a memory item to the active chat session (with deduplication)
 */
export function addChatMemory(
  session: SessionData,
  fact: string
): { added: boolean; count: number } {
  const mem = initSessionMemory(session);
  const item = fact.trim();
  if (!item) return { added: false, count: mem.length };

  // Deduplication check
  if (mem.some(m => m.toLowerCase() === item.toLowerCase())) {
    return { added: false, count: mem.length };
  }

  mem.push(item);
  return { added: true, count: mem.length };
}

/**
 * Remove a memory item matching a query from the active chat session
 */
export function removeChatMemory(
  session: SessionData,
  query: string
): boolean {
  const mem = initSessionMemory(session);
  const lower = query.toLowerCase().trim();
  if (!lower) return false;

  const initialLen = mem.length;
  session.memory = mem.filter(m => !m.toLowerCase().includes(lower));
  return session.memory.length < initialLen;
}

/**
 * Clear all memory for the active chat session
 */
export function clearChatMemory(session: SessionData): void {
  session.memory = [];
}

/**
 * Format active chat memory for system prompt inclusion
 */
export function formatChatMemoryForPrompt(session: SessionData): string {
  const mem = initSessionMemory(session);
  if (mem.length === 0) return "";

  const lines = mem.map(m => `- ${m}`).join("\n");
  return `CHAT MEMORY (Context, constraints, and facts established in this chat session):\n${lines}`;
}

/**
 * Auto-detect user introductions, explicit remember instructions, and preferences,
 * and immediately persist them into the active chat session's memory.
 */
export function extractAndSaveAutoMemory(
  session: SessionData,
  text: string
): { remembered: boolean; fact?: string } {
  const trimmed = text.trim();

  // 1. "my name is <name>" / "call me <name>" / "i am <name>"
  const nameMatch = trimmed.match(
    /(?:(?:hi|hello|hey|yo)[, ]+)?(?:my name is|i am|i'm|call me)\s+([a-zA-Z0-9_\- ]+?)(?:[.!,]|$)/i
  );
  if (nameMatch && nameMatch[1]) {
    const rawName = nameMatch[1].trim();
    const blacklist = [
      "a developer",
      "an engineer",
      "here",
      "ready",
      "building",
      "coding",
      "testing",
      "working",
      "fine",
      "good",
    ];
    if (!blacklist.includes(rawName.toLowerCase()) && rawName.length > 1 && rawName.length < 35) {
      const fact = `User's name is ${rawName}`;
      addChatMemory(session, fact);
      return { remembered: true, fact };
    }
  }

  // 2. "remember that <X>" or "remember to <X>" or "remember <X>"
  const rememberMatch = trimmed.match(/^remember(?:\s+that|\s+to)?\s+([^.!?]+)/i);
  if (rememberMatch && rememberMatch[1]) {
    const fact = rememberMatch[1].trim();
    if (fact.length > 3) {
      addChatMemory(session, fact);
      return { remembered: true, fact };
    }
  }

  // 3. "always use <X>" or "i prefer <X>"
  const prefMatch = trimmed.match(/^(?:always use|i prefer|prefer)\s+([^.!?]+)/i);
  if (prefMatch && prefMatch[1]) {
    const fact = trimmed.replace(/[.!]+$/, "").trim();
    addChatMemory(session, fact);
    return { remembered: true, fact };
  }

  return { remembered: false };
}
