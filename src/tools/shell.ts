import { spawn } from "node:child_process";

export interface BashArgs {
  command: string;
  timeout_ms?: number;
}

export async function executeBash(
  workdir: string,
  args: BashArgs
): Promise<{ content: string; exitCode: number; isError: boolean }> {
  const timeoutMs = args.timeout_ms || 120000;

  return new Promise((resolve) => {
    let stdout = "";
    let stderr = "";
    let timedOut = false;

    const child = spawn("bash", ["-c", args.command], {
      cwd: workdir,
      env: { ...process.env, PAGER: "cat", CI: "true" },
    });

    const timer = setTimeout(() => {
      timedOut = true;
      child.kill("SIGKILL");
    }, timeoutMs);

    child.stdout.on("data", (d) => {
      if (stdout.length < 100000) {
        stdout += d.toString();
      }
    });

    child.stderr.on("data", (d) => {
      if (stderr.length < 50000) {
        stderr += d.toString();
      }
    });

    child.on("close", (code) => {
      clearTimeout(timer);
      const exitCode = code ?? (timedOut ? 124 : 1);
      if (timedOut) {
        return resolve({
          content: `Command timed out after ${timeoutMs}ms\nOutput so far:\n${stdout}`,
          exitCode,
          isError: true,
        });
      }

      let combined = stdout;
      if (stderr.trim()) {
        combined += (combined ? "\n" : "") + stderr;
      }

      if (!combined.trim()) {
        combined = exitCode === 0 ? "Command completed with no output" : `Command failed with exit code ${exitCode}`;
      }

      resolve({
        content: combined.trim(),
        exitCode,
        isError: exitCode !== 0,
      });
    });

    child.on("error", (err) => {
      clearTimeout(timer);
      resolve({
        content: `Execution error: ${err.message}`,
        exitCode: 1,
        isError: true,
      });
    });
  });
}
