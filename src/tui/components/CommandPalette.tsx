import React from "react";
import { Box, Text } from "ink";
import SelectInput from "ink-select-input";

export interface CommandItem {
  label: string;
  value: string;
}

export const defaultCommands: CommandItem[] = [
  { label: "󰘧 /models    - List and switch available AI models", value: "/models" },
  { label: "⚡ /connect   - Configure model backends (Ollama, OpenRouter)", value: "/connect" },
  { label: "🧹 /clear     - Clear current conversation viewport", value: "/clear" },
  { label: "📂 /sessions  - Browse and restore saved agent sessions", value: "/sessions" },
  { label: "📊 /status    - Inspect workspace, git diffs & tokens", value: "/status" },
  { label: "❓ /help      - Show keyboard shortcuts & command cheat-sheet", value: "/help" },
  { label: "🚪 /exit      - Quit Zeuf", value: "/exit" },
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
      borderColor="cyan"
      flexDirection="column"
      paddingX={1}
      marginY={1}
    >
      <Box justifyContent="space-between" marginBottom={1}>
        <Text bold color="cyan">◈ COMMAND PALETTE</Text>
        <Text color="gray">[↑↓: Select | Enter: Run | Esc: Dismiss]</Text>
      </Box>
      {filtered.length === 0 ? (
        <Text color="gray">No matching commands</Text>
      ) : (
        <SelectInput items={filtered} onSelect={onSelect} />
      )}
    </Box>
  );
};
