import React from "react";
import { Box, Text } from "ink";
import Spinner from "ink-spinner";

export interface ThinkingCardProps {
  duration?: number;
  text?: string;
}

export const ThinkingCard: React.FC<ThinkingCardProps> = ({ duration, text }) => {
  const durStr = duration ? `${(duration / 1000).toFixed(1)}s` : "Thinking…";

  return (
    <Box flexDirection="column" paddingLeft={2} marginY={1}>
      <Box gap={1}>
        <Text color="cyan"><Spinner type="dots" /></Text>
        <Text color="gray">Thought for {durStr}</Text>
      </Box>
      {text && text.trim() && (
        <Box
          borderStyle="single"
          borderLeft={true}
          borderTop={false}
          borderRight={false}
          borderBottom={false}
          paddingLeft={1}
          marginTop={1}
        >
          <Text color="gray">{text.slice(0, 300)}</Text>
        </Box>
      )}
    </Box>
  );
};
