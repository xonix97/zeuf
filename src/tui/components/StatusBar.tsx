import React from "react";
import { Box, Text } from "ink";
import Spinner from "ink-spinner";

export interface StatusBarProps {
  model: string;
  busy: boolean;
  branch?: string;
  tokens: number;
}

export const StatusBar: React.FC<StatusBarProps> = ({
  model,
  busy,
  branch,
  tokens,
}) => {
  const modelShort = model.replace(/^opencode\//, "");

  return (
    <Box
      borderStyle="single"
      borderLeft={false}
      borderRight={false}
      borderBottom={false}
      borderColor="gray"
      paddingX={1}
      justifyContent="space-between"
    >
      <Box gap={1}>
        <Text bold backgroundColor="#005f87" color="white"> ◈ ZEUF </Text>
        <Text bold backgroundColor="#333333" color="cyan"> 󰚩 {modelShort} </Text>
        {busy ? (
          <Box gap={1}>
            <Text color="yellow"><Spinner type="dots" /></Text>
            <Text bold color="yellow">WORKING</Text>
          </Box>
        ) : (
          <Text bold color="green">● READY</Text>
        )}
      </Box>
      <Box gap={2}>
        {branch && <Text color="yellow">⎇ {branch}</Text>}
        {tokens > 0 && <Text color="gray">{tokens} tok</Text>}
        <Text color="gray">^P models | / help</Text>
      </Box>
    </Box>
  );
};
