import React from "react";
import { render } from "ink";
import { App } from "./App";

export async function runTUI(workdir: string = process.cwd(), autoApprove: boolean = false, sessionId?: string): Promise<void> {
  // Enter alternate screen buffer
  process.stdout.write("\x1b[?1049h");
  process.stdout.write("\x1b[2J\x1b[H");

  const appInstance = render(<App workdir={workdir} autoApprove={autoApprove} sessionId={sessionId} />);

  await appInstance.waitUntilExit();

  // Restore main screen buffer
  process.stdout.write("\x1b[?1049l");
}
