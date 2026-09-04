import React from "react";
import { Box, Text, useInput } from "ink";
import { editorial } from "../editorialTheme";

export interface ApprovalPromptProps {
  toolName: string;
  argsJSON: string;
  onDecision: (decision: "allow" | "always" | "deny") => void;
}

export const ApprovalPrompt: React.FC<ApprovalPromptProps> = ({
  toolName,
  argsJSON,
  onDecision,
}) => {
  useInput((input, key) => {
    if (input === "y" || input === "Y") {
      onDecision("allow");
    } else if (input === "a" || input === "A") {
      onDecision("always");
    } else if (input === "n" || input === "N" || key.escape) {
      onDecision("deny");
    }
  });

  return (
    <Box
      borderStyle="round"
      borderColor={editorial.gold}
      flexDirection="column"
      paddingX={1}
      marginY={1}
    >
      <Box marginBottom={1}>
        <Text bold backgroundColor={editorial.gold} color={editorial.ink}> ⚠️ PERMISSION REQUIRED </Text>
      </Box>
      <Text color={editorial.creamDim}>Tool: <Text bold color={editorial.cream}>{toolName}</Text></Text>
      <Text color={editorial.creamMute}>Args: {argsJSON.slice(0, 140)}</Text>
      <Box marginTop={1} gap={2}>
        <Text bold backgroundColor={editorial.paper} color={editorial.ink}> [y] Allow </Text>
        <Text bold backgroundColor={editorial.line2} color={editorial.cream}> [a] Always </Text>
        <Text bold color={editorial.rust}> [n/Esc] Deny </Text>
      </Box>
    </Box>
  );
};
