import React from "react";
import { Box, Text } from "ink";
import TextInput from "ink-text-input";
import { editorial } from "../editorialTheme";

export interface PromptInputProps {
  value: string;
  onChange: (value: string) => void;
  onSubmit: (value: string) => void;
  disabled?: boolean;
}

export const PromptInput: React.FC<PromptInputProps> = ({
  value,
  onChange,
  onSubmit,
  disabled = false,
}) => {
  return (
    <Box
      borderStyle="round"
      borderColor={editorial.cream}
      flexDirection="column"
      paddingX={1}
    >
      <Box justifyContent="space-between">
        <Text bold backgroundColor={editorial.paper} color={editorial.ink}> 💬 PROMPT </Text>
        <Text color={editorial.creamMute}>[Enter: Send | /: Commands | ^P: Models | ^C: Exit]</Text>
      </Box>
      <Box marginTop={0}>
        <Text bold color={editorial.gold}>› </Text>
        <TextInput
          value={value}
          onChange={onChange}
          onSubmit={onSubmit}
          focus={!disabled}
          placeholder={disabled ? "Agent is working..." : "Ask Zeuf to write code, refactor, or run tests..."}
        />
      </Box>
    </Box>
  );
};
