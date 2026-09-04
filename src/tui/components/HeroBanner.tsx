import React from "react";
import { Box, Text } from "ink";

export interface HeroBannerProps {
  model: string;
  branch?: string;
}

export const HeroBanner: React.FC<HeroBannerProps> = ({ model, branch }) => {
  return (
    <Box flexDirection="column" alignItems="center" marginY={1}>
      <Text bold color="cyan">◈   Z E U F   A R C H I T E C T   ◈</Text>
      <Text color="gray">Autonomous AI Engineering Agent • High-Performance Bun Runtime</Text>
      <Text color="gray">
        Connected: <Text color="green">{model.replace(/^opencode\//, "")}</Text>
        {branch && <Text> • Branch: <Text color="yellow">{branch}</Text></Text>}
        {" • v0.5.0"}
      </Text>

      <Box gap={2} marginTop={1}>
        <Box
          borderStyle="round"
          borderColor="cyan"
          flexDirection="column"
          paddingX={2}
          width={38}
        >
          <Text bold color="cyan">⚡ Capabilities</Text>
          <Text color="white">• Autonomous code edits</Text>
          <Text color="white">• Shell & git automation</Text>
          <Text color="white">• Fast-path direct chat</Text>
          <Text color="white">• Multi-model fallback</Text>
        </Box>

        <Box
          borderStyle="round"
          borderColor="cyan"
          flexDirection="column"
          paddingX={2}
          width={38}
        >
          <Text bold color="cyan">⌨ Shortcuts</Text>
          <Text><Text color="yellow">[ / ] </Text><Text color="gray">Command palette</Text></Text>
          <Text><Text color="yellow">[^P] </Text><Text color="gray">Switch AI model</Text></Text>
          <Text><Text color="yellow">[^C] </Text><Text color="gray">Cancel/interrupt</Text></Text>
          <Text><Text color="yellow">[Esc] </Text><Text color="gray">Close overlays</Text></Text>
        </Box>
      </Box>

      <Box marginTop={1}>
        <Text color="gray">
          💡 Tip: Type your task or question below, or type <Text color="yellow">/</Text> for commands
        </Text>
      </Box>
    </Box>
  );
};
