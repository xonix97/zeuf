import React from "react";
import { Box, Text } from "ink";
import { editorial } from "../editorialTheme";

export interface HeroBannerProps {
  model: string;
  branch?: string;
}

export const HeroBanner: React.FC<HeroBannerProps> = ({ model, branch }) => {
  return (
    <Box flexDirection="column" alignItems="center" marginY={1}>
      <Text color={editorial.creamMute} bold>
        ONE AGENT · MANY MODEL SOURCES · YOUR MACHINE
      </Text>
      <Box marginY={1}>
        <Text bold color={editorial.cream}>
          z  e  u  f  .
        </Text>
      </Box>
      <Text italic color={editorial.creamDim}>
        "Not a chat wrapper — a full agentic coding environment that lives in your terminal."
      </Text>
      <Box marginTop={1}>
        <Text color={editorial.creamMute}>
          Connected: <Text color={editorial.sage}>{model.replace(/^opencode\//, "")}</Text>
          {branch && <Text>  ·  Branch: <Text color={editorial.gold}>{branch}</Text></Text>}
          {"  ·  v0.5.0"}
        </Text>
      </Box>

      <Box gap={2} marginTop={1}>
        <Box
          borderStyle="round"
          borderColor={editorial.line2}
          flexDirection="column"
          paddingX={2}
          width={40}
        >
          <Box justifyContent="space-between" marginBottom={1}>
            <Text bold color={editorial.cream}>01 — MANIFEST</Text>
            <Text color={editorial.creamMute}>CORE</Text>
          </Box>
          <Text color={editorial.creamSoft}>• Smart model router (auto-switch)</Text>
          <Text color={editorial.creamSoft}>• Seamless fallback (zero loss)</Text>
          <Text color={editorial.creamSoft}>• Real tool execution (bounded)</Text>
          <Text color={editorial.creamSoft}>• Approvals hub (sensitive gate)</Text>
        </Box>

        <Box
          borderStyle="round"
          borderColor={editorial.line2}
          flexDirection="column"
          paddingX={2}
          width={40}
        >
          <Box justifyContent="space-between" marginBottom={1}>
            <Text bold color={editorial.cream}>02 — SHORTCUTS</Text>
            <Text color={editorial.creamMute}>NAV</Text>
          </Box>
          <Text><Text color={editorial.gold}>[ / ]   </Text><Text color={editorial.creamDim}>01  Command palette</Text></Text>
          <Text><Text color={editorial.gold}>[^P]   </Text><Text color={editorial.creamDim}>02  Switch AI model</Text></Text>
          <Text><Text color={editorial.gold}>[^C]   </Text><Text color={editorial.creamDim}>03  Interrupt / cancel</Text></Text>
          <Text><Text color={editorial.gold}>[Esc]  </Text><Text color={editorial.creamDim}>04  Close active modal</Text></Text>
        </Box>
      </Box>

      <Box marginTop={1}>
        <Text color={editorial.creamMute}>
          Type your task below, or press <Text bold color={editorial.cream}>/</Text> for commands
        </Text>
      </Box>
    </Box>
  );
};
