import type { SubItem } from "./api";

const TYPES = ["epic", "story", "task", "bug"];

// One subitem per line as "type: title"; two spaces of indent per level make a child of the line above.
export function parseSubitems(text: string): SubItem[] {
  const root: SubItem[] = [];
  const stack: { depth: number; list: SubItem[] }[] = [
    { depth: -1, list: root },
  ];
  for (const raw of text.split("\n")) {
    if (!raw.trim()) continue;
    const depth = Math.floor((raw.length - raw.trimStart().length) / 2);
    const line = raw.trim();
    const colon = line.indexOf(":");
    const typed =
      colon > 0 && TYPES.includes(line.slice(0, colon).trim().toLowerCase());
    const item: SubItem = {
      type: typed ? line.slice(0, colon).trim().toLowerCase() : "task",
      title: (typed ? line.slice(colon + 1) : line).trim(),
    };
    while (stack.length > 1 && (stack[stack.length - 1]?.depth ?? 0) >= depth)
      stack.pop();
    const parent = stack[stack.length - 1];
    if (!parent) continue;
    const children: SubItem[] = [];
    item.children = children;
    parent.list.push(item);
    stack.push({ depth, list: children });
  }
  return root;
}

export function formatSubitems(list: SubItem[], depth = 0): string {
  return list
    .flatMap((s) => [
      `${"  ".repeat(depth)}${s.type}: ${s.title}`,
      ...(s.children && s.children.length > 0
        ? [formatSubitems(s.children, depth + 1)]
        : []),
    ])
    .join("\n");
}
