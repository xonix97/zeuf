import { describe, it, expect } from "bun:test";
import React from "react";
import { render } from "ink-testing-library";
import { Header } from "../src/tui/components/Header";
import { HeroBanner } from "../src/tui/components/HeroBanner";
import { MessageCard } from "../src/tui/components/MessageCard";
import { ThinkingCard } from "../src/tui/components/ThinkingCard";
import { ToolCard } from "../src/tui/components/ToolCard";
import { StatusBar } from "../src/tui/components/StatusBar";
import { CommandPalette } from "../src/tui/components/CommandPalette";

describe("Zeuf React Ink Components", () => {
  it("renders Header without throwing", () => {
    const { unmount, lastFrame } = render(
      <Header workdir="/home/archlinux/zeuf" branch="master" model="opencode/big-pickle" />
    );
    const frame = lastFrame();
    expect(frame).toContain("zeuf.");
    expect(frame).toContain("master");
    unmount();
  });

  it("renders HeroBanner with manifest, shortcut cards, and recent tasks", () => {
    const { unmount, lastFrame } = render(
      <HeroBanner model="opencode/big-pickle" branch="master" />
    );
    const frame = lastFrame();
    expect(frame).toContain("zeuf.");
    expect(frame).toContain("ONE AGENT · MANY MODEL SOURCES · YOUR MACHINE");
    expect(frame).toContain("MANIFEST");
    expect(frame).toContain("SHORTCUTS");
    expect(frame).toContain("RECENT TASKS");
    unmount();
  });

  it("renders HeroBanner with tasteful empty state when no recent tasks exist", () => {
    const { unmount, lastFrame } = render(
      <HeroBanner model="opencode/big-pickle" branch="master" recentTasks={[]} />
    );
    const frame = lastFrame();
    expect(frame).toContain("No recent tasks");
    expect(frame).toContain("Start by describing what you want Zeuf to build.");
    unmount();
  });

  it("renders MessageCards for user and assistant", () => {
    const { unmount, lastFrame } = render(
      <>
        <MessageCard role="user" text="Hello world" timestamp="10:00 PM" />
        <MessageCard role="assistant" text="Hello from Zeuf" model="big-pickle" timestamp="10:00 PM" />
      </>
    );
    const frame = lastFrame();
    expect(frame).toContain("YOU");
    expect(frame).toContain("Hello world");
    expect(frame).toContain("zeuf.");
    expect(frame).toContain("Hello from Zeuf");
    unmount();
  });

  it("renders ThinkingCard and ToolCard with status badges", () => {
    const { unmount, lastFrame } = render(
      <>
        <ThinkingCard duration={1200} text="Thinking details" />
        <ToolCard toolName="bash" args="echo 1" toolOk={true} durationMs={45} />
      </>
    );
    const frame = lastFrame();
    expect(frame).toContain("Thought for 1.2s");
    expect(frame).toContain("bash");
    expect(frame).toContain("COMPLETE");
    unmount();
  });

  it("renders CommandPalette with slash commands", () => {
    const { unmount, lastFrame } = render(
      <CommandPalette filter="/mod" onSelect={() => {}} />
    );
    const frame = lastFrame();
    expect(frame).toContain("COMMAND PALETTE");
    expect(frame).toContain("/models");
    unmount();
  });

  it("renders StatusBar with model pill and readiness", () => {
    const { unmount, lastFrame } = render(
      <StatusBar model="opencode/big-pickle" busy={false} branch="master" tokens={150} />
    );
    const frame = lastFrame();
    expect(frame).toContain("zeuf.");
    expect(frame).toContain("READY");
    expect(frame).toContain("150 tok");
    unmount();
  });
});
