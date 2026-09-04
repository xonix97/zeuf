export function stripAnsi(str: string): string {
  return str.replace(/\x1b\[[0-9;]*[a-zA-Z]/g, "");
}

export function visibleWidth(str: string): number {
  return stripAnsi(str).length;
}

export function truncate(str: string, maxWidth: number): string {
  const visible = visibleWidth(str);
  if (visible <= maxWidth) return str;

  let count = 0;
  let result = "";
  let inEscape = false;

  for (let i = 0; i < str.length; i++) {
    const char = str[i];
    if (char === "\x1b") {
      inEscape = true;
      result += char;
      continue;
    }
    if (inEscape) {
      result += char;
      if (char >= "a" && char <= "z" || char >= "A" && char <= "Z") {
        inEscape = false;
      }
      continue;
    }

    if (count < maxWidth - 1) {
      result += char;
      count++;
    } else {
      result += "…";
      break;
    }
  }

  return result;
}

export function padRight(str: string, targetWidth: number): string {
  const len = visibleWidth(str);
  if (len >= targetWidth) return truncate(str, targetWidth);
  return str + " ".repeat(targetWidth - len);
}

export function padLeft(str: string, targetWidth: number): string {
  const len = visibleWidth(str);
  if (len >= targetWidth) return truncate(str, targetWidth);
  return " ".repeat(targetWidth - len) + str;
}

export function padCenter(str: string, targetWidth: number): string {
  const len = visibleWidth(str);
  if (len >= targetWidth) return truncate(str, targetWidth);
  const left = Math.floor((targetWidth - len) / 2);
  const right = targetWidth - len - left;
  return " ".repeat(left) + str + " ".repeat(right);
}
