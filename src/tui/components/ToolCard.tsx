import React from "react";
import { Box, Text } from "ink";
import Spinner from "ink-spinner";
import { editorial } from "../editorialTheme";

export interface ToolCardProps {
  toolName: string;
  args?: string;
  toolOk?: boolean;
  durationMs?: number;
  output?: string;
}

export const ToolCard: React.FC<ToolCardProps> = ({
  toolName,
  args,
  toolOk,
  durationMs,
  output,
}) => {
  const isRunning = toolOk === undefined;
  const isSuccess = toolOk === true;
  const borderColor = isRunning ? editorial.gold : isSuccess ? editorial.line2 : editorial.rust;
  const icon = isRunning ? null : isSuccess ? "✓" : "✗";

  return (
    <Box
      borderStyle="round"
      borderColor={borderColor}
      flexDirection="column"
      paddingX={1}
      marginY={1}
    >
      <Box justifyContent="space-between">
        <Box gap={1}>
          {isRunning ? (
            <Text color={editorial.gold}><Spinner type="dots" /></Text>
          ) : (
            <Text bold color={isSuccess ? editorial.sage : editorial.rust}>{icon}</Text>
          )}
          <Text bold color={editorial.cream}>{toolName}</Text>
          {durationMs !== undefined && <Text color={editorial.creamMute}>({durationMs}ms)</Text>}
        </Box>
        {isRunning && (
          <Text bold backgroundColor={editorial.line2} color={editorial.gold}> EXECUTING </Text>
        )}
        {isSuccess && (
          <Text bold backgroundColor="#1a2215" color={editorial.sage}> COMPLETE </Text>
        )}
        {toolOk === false && (
          <Text bold backgroundColor="#281410" color={editorial.rust}> FAILED </Text>
        )}
      </Box>

      {args && (
        <Box marginTop={1}>
          <Text color={editorial.creamDim}>args: {args.slice(0, 120)}</Text>
        </Box>
      )}

      {output && output.trim() && (
        <Box marginTop={1}>
          <Text color={editorial.creamMute}>{output.slice(0, 240)}</Text>
        </Box>
      )}
    </Box>
  );
};
