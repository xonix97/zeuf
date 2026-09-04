import React from "react";
import { Box, Text } from "ink";
import Spinner from "ink-spinner";

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
  const borderColor = isRunning ? "yellow" : isSuccess ? "green" : "red";
  const statusText = isRunning ? "RUNNING" : isSuccess ? "SUCCESS" : "FAILED";
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
            <Text color="yellow"><Spinner type="dots" /></Text>
          ) : (
            <Text bold color={borderColor}>{icon}</Text>
          )}
          <Text bold color={borderColor}>{toolName}</Text>
          {durationMs !== undefined && <Text color="gray">({durationMs}ms)</Text>}
        </Box>
        <Text bold color={borderColor}>[{statusText}]</Text>
      </Box>

      {args && (
        <Box marginTop={1}>
          <Text color="gray">args: {args.slice(0, 100)}</Text>
        </Box>
      )}

      {output && output.trim() && (
        <Box marginTop={1}>
          <Text color="gray">{output.slice(0, 200)}</Text>
        </Box>
      )}
    </Box>
  );
};
