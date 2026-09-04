import React from "react";
import { Box, Text } from "ink";

export interface HeaderProps {
  workdir: string;
  branch?: string;
  model: string;
}

export const Header: React.FC<HeaderProps> = ({ workdir, branch, model }) => {
  const home = process.env.HOME || "";
  const displayDir = workdir.startsWith(home) ? "~" + workdir.slice(home.length) : workdir;

  return (
    <Box
      borderStyle="round"
      borderColor="cyan"
      paddingX={1}
      justifyContent="space-between"
    >
      <Box gap={1}>
        <Text bold color="cyan">◈ ZEUF ARCHITECT</Text>
        <Text color="gray">v0.5.0</Text>
      </Box>
      <Box gap={2}>
        <Text color="gray">📁 {displayDir}</Text>
        {branch && <Text color="yellow">⎇ {branch}</Text>}
        <Text color="green">● {model.replace(/^opencode\//, "")}</Text>
      </Box>
    </Box>
  );
};
