import { existsSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { resolve, dirname, relative } from "node:path";

export interface ReadArgs {
  path: string;
  offset?: number;
  limit?: number;
}

export interface WriteArgs {
  path: string;
  content: string;
}

export interface EditArgs {
  path: string;
  old_string: string;
  new_string: string;
}

export function readFile(workdir: string, args: ReadArgs): { content: string; isError: boolean } {
  try {
    const full = resolve(workdir, args.path);
    if (!existsSync(full)) {
      return { content: `Error: file not found: ${args.path}`, isError: true };
    }
    const raw = readFileSync(full, "utf-8");
    const lines = raw.split("\n");
    const offset = Math.max(0, (args.offset || 1) - 1);
    const limit = args.limit || 2000;
    const slice = lines.slice(offset, offset + limit);
    return { content: slice.join("\n"), isError: false };
  } catch (err: any) {
    return { content: `Error reading file: ${err.message}`, isError: true };
  }
}

export function writeFile(workdir: string, args: WriteArgs): { content: string; isError: boolean } {
  try {
    const full = resolve(workdir, args.path);
    const dir = dirname(full);
    if (!existsSync(dir)) {
      mkdirSync(dir, { recursive: true });
    }
    writeFileSync(full, args.content, "utf-8");
    const rel = relative(workdir, full);
    return { content: `Successfully wrote ${args.content.length} bytes to ${rel}`, isError: false };
  } catch (err: any) {
    return { content: `Error writing file: ${err.message}`, isError: true };
  }
}

export function editFile(workdir: string, args: EditArgs): { content: string; isError: boolean } {
  try {
    const full = resolve(workdir, args.path);
    if (!existsSync(full)) {
      return { content: `Error: file not found: ${args.path}`, isError: true };
    }
    const raw = readFileSync(full, "utf-8");
    const count = raw.split(args.old_string).length - 1;
    if (count === 0) {
      return { content: `Error: old_string not found in ${args.path}`, isError: true };
    }
    if (count > 1) {
      return { content: `Error: old_string occurs ${count} times in ${args.path}, must be unique`, isError: true };
    }
    const updated = raw.replace(args.old_string, args.new_string);
    writeFileSync(full, updated, "utf-8");
    return { content: `Successfully replaced content in ${args.path}`, isError: false };
  } catch (err: any) {
    return { content: `Error editing file: ${err.message}`, isError: true };
  }
}
