import type { ToolDefinition, ToolResult } from "../core/types";
import { readFile, writeFile, editFile, type ReadArgs, type WriteArgs, type EditArgs } from "./file";
import { executeBash, type BashArgs } from "./shell";
import { executeGlob, executeGrep, type GlobArgs, type GrepArgs } from "./search";
import { gitDiff, gitStatus } from "./git";

export type ApproverFn = (toolName: string, argsJSON: string) => Promise<"allow" | "always" | "deny">;

export class ToolRegistry {
  workdir: string;
  autoApprove: boolean;
  approver?: ApproverFn;
  alwaysAllowed: Set<string> = new Set();
  modifiedFiles: Set<string> = new Set();

  constructor(workdir: string = process.cwd(), autoApprove: boolean = false, approver?: ApproverFn) {
    this.workdir = workdir;
    this.autoApprove = autoApprove;
    this.approver = approver;
  }

  definitions(): ToolDefinition[] {
    return [
      {
        name: "read",
        description: "Read the contents of a file at the given path. Supports optional line offset and limit.",
        parameters: {
          type: "object",
          properties: {
            path: { type: "string", description: "Relative or absolute file path" },
            offset: { type: "number", description: "1-based starting line number (default: 1)" },
            limit: { type: "number", description: "Maximum number of lines to read (default: 2000)" },
          },
          required: ["path"],
        },
      },
      {
        name: "write",
        description: "Write content to a file at the specified path. Creates missing parent directories automatically.",
        parameters: {
          type: "object",
          properties: {
            path: { type: "string", description: "File path to create or overwrite" },
            content: { type: "string", description: "Full file content to write" },
          },
          required: ["path", "content"],
        },
      },
      {
        name: "edit",
        description: "Replace exactly one unique occurrence of old_string with new_string in the file.",
        parameters: {
          type: "object",
          properties: {
            path: { type: "string", description: "File path to edit" },
            old_string: { type: "string", description: "Exact character sequence to be replaced (must be unique)" },
            new_string: { type: "string", description: "Replacement text" },
          },
          required: ["path", "old_string", "new_string"],
        },
      },
      {
        name: "bash",
        description: "Execute a bash command in the project working directory.",
        parameters: {
          type: "object",
          properties: {
            command: { type: "string", description: "The bash command line to execute" },
            timeout_ms: { type: "number", description: "Optional execution timeout in milliseconds (default: 120000)" },
          },
          required: ["command"],
        },
      },
      {
        name: "glob",
        description: "Search for files matching a wildcard pattern.",
        parameters: {
          type: "object",
          properties: {
            pattern: { type: "string", description: "Filename pattern like '*.ts' or 'auth*'" },
            dir: { type: "string", description: "Directory to search within (default: .)" },
          },
          required: ["pattern"],
        },
      },
      {
        name: "grep",
        description: "Search file contents using regular expressions or text matching.",
        parameters: {
          type: "object",
          properties: {
            pattern: { type: "string", description: "Search pattern" },
            dir: { type: "string", description: "Directory to search within (default: .)" },
            includes: { type: "string", description: "Optional file glob filter like '*.go'" },
          },
          required: ["pattern"],
        },
      },
    ];
  }

  async execute(toolCallId: string, name: string, argsJSON: string): Promise<ToolResult> {
    let args: any = {};
    try {
      args = JSON.parse(argsJSON);
    } catch (e: any) {
      return { toolCallId, name, content: `Error: invalid JSON arguments: ${e.message}`, isError: true };
    }

    // Permission check
    const isDestructive = name === "write" || name === "edit" || name === "bash";
    if (isDestructive && !this.autoApprove && !this.alwaysAllowed.has(name) && this.approver) {
      const decision = await this.approver(name, argsJSON);
      if (decision === "deny") {
        return { toolCallId, name, content: "Tool execution was denied by the user.", isError: true };
      }
      if (decision === "always") {
        this.alwaysAllowed.add(name);
      }
    }

    switch (name) {
      case "read": {
        const res = readFile(this.workdir, args as ReadArgs);
        return { toolCallId, name, content: res.content, isError: res.isError };
      }
      case "write": {
        const res = writeFile(this.workdir, args as WriteArgs);
        if (!res.isError) {
          this.modifiedFiles.add(args.path);
        }
        return { toolCallId, name, content: res.content, isError: res.isError };
      }
      case "edit": {
        const res = editFile(this.workdir, args as EditArgs);
        if (!res.isError) {
          this.modifiedFiles.add(args.path);
        }
        return { toolCallId, name, content: res.content, isError: res.isError };
      }
      case "bash": {
        const res = await executeBash(this.workdir, args as BashArgs);
        return { toolCallId, name, content: res.content, isError: res.isError };
      }
      case "glob": {
        const res = await executeGlob(this.workdir, args as GlobArgs);
        return { toolCallId, name, content: res.content, isError: res.isError };
      }
      case "grep": {
        const res = await executeGrep(this.workdir, args as GrepArgs);
        return { toolCallId, name, content: res.content, isError: res.isError };
      }
      default:
        return { toolCallId, name, content: `Unknown tool: ${name}`, isError: true };
    }
  }
}
