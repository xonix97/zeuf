import React from "react";
import { Box, Text } from "ink";
import Spinner from "ink-spinner";
import { editorial } from "../editorialTheme";

export interface ThinkingCardProps {
  duration?: number;
  text?: string;
}

export const ThinkingCard: React.FC<ThinkingCardProps> = ({ duration, text }) => {
  const durStr = duration ? `${(duration / 1000).toFixed(1)}s` : "Thinking…";

  return (
    <Box flexDirection="column" paddingLeft={2} marginY={1}>
      <Box gap={1}>
        <Text color={editorial.cream}><Spinner type="dots" /></Text>
        <Text italic color={editorial.creamDim}>Thought for {durStr}</Text>
      </Box>
      {text && text.trim() && (
        <Box
          borderStyle="single"
          borderLeft={true}
          borderTop={false}
          borderRight={false}
          borderBottom={false}
          borderColor={editorial.line2}
          paddingLeft={1}
          marginTop={1}
        >
          <Text color={editorial.creamMute}>{text.slice(0, 300)}</Text>
        </Box>
      )}
    </Box>
  );
};
