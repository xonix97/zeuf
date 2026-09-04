import React from "react";
import { Box, Text } from "ink";
import { editorial } from "../editorialTheme";

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
        borderColor={editorial.line2}
        flexDirection="column"
        paddingX={1}
        marginY={1}
      >
        <Box justifyContent="space-between">
          <Text bold color={editorial.gold}> YOU</Text>
          {timestamp && <Text color={editorial.creamMute}>{timestamp}</Text>}
        </Box>
        <Text bold color={editorial.cream}>{text}</Text>
      </Box>
    );
  }

  if (role === "assistant") {
    return (
      <Box
        borderStyle="round"
        borderColor={editorial.cream}
        flexDirection="column"
        paddingX={1}
        marginY={1}
      >
        <Box justifyContent="space-between">
          <Box gap={1}>
            <Text bold backgroundColor={editorial.paper} color={editorial.ink}> zeuf. </Text>
            {model && <Text color={editorial.creamDim}>[{model.replace(/^opencode\//, "")}]</Text>}
          </Box>
          {timestamp && <Text color={editorial.creamMute}>{timestamp}</Text>}
        </Box>
        <Text color={editorial.creamSoft}>{text}</Text>
      </Box>
    );
  }

  if (role === "error") {
    return (
      <Box
        borderStyle="round"
        borderColor={editorial.rust}
        flexDirection="column"
        paddingX={1}
        marginY={1}
      >
        <Box justifyContent="space-between">
          <Text bold backgroundColor={editorial.rust} color={editorial.ink}> ERROR </Text>
          {timestamp && <Text color={editorial.creamMute}>{timestamp}</Text>}
        </Box>
        <Text color={editorial.rust}>{text}</Text>
      </Box>
    );
  }

  return (
    <Box marginY={1} paddingLeft={1}>
      <Text color={editorial.creamMute}>✦ {text}</Text>
    </Box>
  );
};
