import React from "react";
import { Box, Text } from "ink";
import TextInput from "ink-text-input";

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
      borderColor="cyan"
      flexDirection="column"
      paddingX={1}
    >
      <Box justifyContent="space-between">
        <Text bold color="white">💬 Prompt</Text>
        <Text color="gray">[Enter: Send | /: Commands | ^P: Models | ^C: Exit]</Text>
      </Box>
      <Box marginTop={0}>
        <Text color="yellow">› </Text>
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
