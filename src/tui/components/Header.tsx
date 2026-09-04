import React from "react";
import { Box, Text } from "ink";
import { editorial } from "../editorialTheme";

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
      borderColor={editorial.line2}
      paddingX={1}
      justifyContent="space-between"
    >
      <Box gap={1}>
        <Text bold backgroundColor={editorial.paper} color={editorial.ink}> zeuf. </Text>
        <Text color={editorial.creamMute}>— YOUR OWN CODING AGENT</Text>
      </Box>
      <Box gap={2}>
        <Text color={editorial.creamDim}>📁 {displayDir}</Text>
        {branch && <Text color={editorial.gold}>⎇ {branch}</Text>}
        <Text color={editorial.sage}>● {model.replace(/^opencode\//, "")}</Text>
        <Text color={editorial.creamMute}>v0.5.0</Text>
      </Box>
    </Box>
  );
};
