import React from "react";
import { Box, Text, useInput } from "ink";

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
      borderColor="yellow"
      flexDirection="column"
      paddingX={1}
      marginY={1}
    >
      <Text bold color="yellow">⚠️ TOOL EXECUTION PERMISSION REQUIRED</Text>
      <Text color="white">Action: <Text bold color="cyan">{toolName}</Text></Text>
      <Text color="gray">Args: {argsJSON.slice(0, 120)}</Text>
      <Box marginTop={1} gap={2}>
        <Text bold color="green">[y] Allow Once</Text>
        <Text bold color="yellow">[a] Always Allow</Text>
        <Text bold color="red">[n/Esc] Deny</Text>
      </Box>
    </Box>
  );
};
