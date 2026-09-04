import React from "react";
import { Box, Text } from "ink";
import Spinner from "ink-spinner";
import { editorial } from "../editorialTheme";

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
      borderColor={editorial.line}
      paddingX={1}
      justifyContent="space-between"
    >
      <Box gap={1}>
        <Text bold backgroundColor={editorial.paper} color={editorial.ink}> zeuf. </Text>
        <Text bold backgroundColor={editorial.surface2} color={editorial.creamSoft}> {modelShort} </Text>
        {busy ? (
          <Box gap={1}>
            <Text color={editorial.gold}><Spinner type="dots" /></Text>
            <Text bold color={editorial.gold}>WORKING</Text>
          </Box>
        ) : (
          <Text bold color={editorial.sage}>● READY</Text>
        )}
      </Box>
      <Box gap={2}>
        {branch && <Text color={editorial.gold}>⎇ {branch}</Text>}
        {tokens > 0 && <Text color={editorial.creamMute}>{tokens} tok</Text>}
        <Text color={editorial.creamMute}>^P models  │  / help</Text>
      </Box>
    </Box>
  );
};
