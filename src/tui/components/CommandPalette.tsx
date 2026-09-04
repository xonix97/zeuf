import React from "react";
import { Box, Text } from "ink";
import SelectInput from "ink-select-input";
import { editorial } from "../editorialTheme";

export interface CommandItem {
  label: string;
  value: string;
}

export const defaultCommands: CommandItem[] = [
  { label: "01  /models    — List and switch available AI models", value: "/models" },
  { label: "02  /connect   — Configure backends (Ollama localhost:11434, OpenRouter)", value: "/connect" },
  { label: "03  /memory    — Inspect persistent project & global memory", value: "/memory" },
  { label: "04  /sessions  — Browse and restore saved agent sessions", value: "/sessions" },
  { label: "05  /status    — Inspect workspace, git diffs & tokens", value: "/status" },
  { label: "06  /clear     — Clear conversation viewport", value: "/clear" },
  { label: "07  /help      — Show keyboard shortcuts & command cheat-sheet", value: "/help" },
  { label: "08  /exit      — Quit Zeuf", value: "/exit" },
];

export interface CommandPaletteProps {
  filter: string;
  onSelect: (item: CommandItem) => void;
}

export const CommandPalette: React.FC<CommandPaletteProps> = ({ filter, onSelect }) => {
  const query = filter.startsWith("/") ? filter.slice(1).toLowerCase().trim() : filter.toLowerCase().trim();
  const filtered = defaultCommands.filter(c =>
    c.value.toLowerCase().includes(query) || c.label.toLowerCase().includes(query)
  );

  return (
    <Box
      borderStyle="round"
      borderColor={editorial.cream}
      flexDirection="column"
      paddingX={1}
      marginY={1}
    >
      <Box justifyContent="space-between" marginBottom={1}>
        <Text bold backgroundColor={editorial.paper} color={editorial.ink}> 01 — COMMAND PALETTE </Text>
        <Text color={editorial.creamMute}>[↑↓: Select | Enter: Run | Esc: Dismiss]</Text>
      </Box>
      {filtered.length === 0 ? (
        <Text color={editorial.creamMute}>No matching commands</Text>
      ) : (
        <SelectInput items={filtered} onSelect={onSelect} />
      )}
    </Box>
  );
};
