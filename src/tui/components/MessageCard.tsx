import React from "react";
import { Box, Text } from "ink";

export interface MessageCardProps {
  role: "user" | "assistant" | "system" | "error";
  text: string;
  model?: string;
  timestamp?: string;
}

export const MessageCard: React.FC<MessageCardProps> = ({ role, text, model, timestamp }) => {
  if (role === "user") {
    return (
      <Box
        borderStyle="round"
        borderColor="yellow"
        flexDirection="column"
        paddingX={1}
        marginY={1}
      >
        <Box justifyContent="space-between">
          <Text bold color="yellow"> You</Text>
          {timestamp && <Text color="gray">{timestamp}</Text>}
        </Box>
        <Text bold color="white">{text}</Text>
      </Box>
    );
  }

  if (role === "assistant") {
    return (
      <Box
        borderStyle="round"
        borderColor="cyan"
        flexDirection="column"
        paddingX={1}
        marginY={1}
      >
        <Box justifyContent="space-between">
          <Text bold color="cyan">◈ Zeuf {model ? `[${model.replace(/^opencode\//, "")}]` : ""}</Text>
          {timestamp && <Text color="gray">{timestamp}</Text>}
        </Box>
        <Text color="white">{text}</Text>
      </Box>
    );
  }

  if (role === "error") {
    return (
      <Box
        borderStyle="round"
        borderColor="red"
        flexDirection="column"
        paddingX={1}
        marginY={1}
      >
        <Text bold color="red">✗ Error</Text>
        <Text color="red">{text}</Text>
      </Box>
    );
  }

  return (
    <Box marginY={1} paddingLeft={1}>
      <Text color="gray">✦ {text}</Text>
    </Box>
  );
};
