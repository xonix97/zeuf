import React, { useState } from "react";
import { Box, Text } from "ink";
import { editorial } from "../editorialTheme";
import { getRecentTasks, type RecentTask } from "../../core/session";

export interface HeroBannerProps {
  model: string;
  branch?: string;
  recentTasks?: RecentTask[];
}

export const HeroBanner: React.FC<HeroBannerProps> = ({
  model,
  branch,
  recentTasks: initialRecentTasks,
}) => {
  const [tasks] = useState<RecentTask[]>(() => initialRecentTasks ?? getRecentTasks(3));

  return (
    <Box flexDirection="column" alignItems="center">
      {/* 1. Compressed Hero / Positioning Area (~30% less vertical whitespace) */}
      <Text color={editorial.creamMute} bold>
        ONE AGENT · MANY MODEL SOURCES · YOUR MACHINE
      </Text>

      <Box gap={1} marginTop={0}>
        <Text bold color={editorial.cream}>zeuf.</Text>
        <Text color={editorial.creamMute}>
          — Connected: <Text color={editorial.sage}>{model.replace(/^opencode\//, "")}</Text>
          {branch && <Text>  ·  Branch: <Text color={editorial.gold}>{branch}</Text></Text>}
          {"  ·  v0.5.0"}
        </Text>
      </Box>

      <Text italic color={editorial.creamDim}>
        "Not a chat wrapper — a full agentic coding environment that lives in your terminal."
      </Text>

      {/* 2. Core Panels: 01 Manifest & 02 Shortcuts */}
      <Box gap={2} marginTop={1} flexWrap="wrap" justifyContent="center">
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

      {/* 3. Terminal-Native Recent Tasks Section */}
      <Box
        borderStyle="round"
        borderColor={editorial.line2}
        flexDirection="column"
        paddingX={2}
        width={82}
        marginTop={1}
      >
        <Box justifyContent="space-between" marginBottom={tasks.length > 0 ? 1 : 0}>
          <Text bold color={editorial.cream}>03 — RECENT TASKS</Text>
          <Text color={editorial.creamMute}>HISTORY</Text>
        </Box>

        {tasks.length === 0 ? (
          <Box flexDirection="column" marginY={1}>
            <Text color={editorial.creamMute}>No recent tasks</Text>
            <Text color={editorial.creamDim}>Start by describing what you want Zeuf to build.</Text>
          </Box>
        ) : (
          tasks.map(t => (
            <Box key={t.id} justifyContent="space-between">
              <Box gap={1}>
                <Text color={editorial.gold}>›</Text>
                <Text color={editorial.creamSoft}>{t.title}</Text>
              </Box>
              <Box gap={1}>
                {t.filesCount > 0 && <Text color={editorial.creamMute}>{t.filesCount} files ·</Text>}
                <Text color={editorial.creamMute}>{t.turns} turns ·</Text>
                <Text color={editorial.creamDim}>{t.timeAgo}</Text>
              </Box>
            </Box>
          ))
        )}
      </Box>

      {/* 4. Bottom Prompt Kicker */}
      <Box marginTop={1}>
        <Text color={editorial.creamMute}>
          Type your task below, or press <Text bold color={editorial.cream}>/</Text> for commands
        </Text>
      </Box>
    </Box>
  );
};
