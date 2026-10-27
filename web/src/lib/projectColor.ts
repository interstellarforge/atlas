// A project keeps one accent everywhere (sidebar, cards): derived from its key, no stored setting.
const ACCENTS = [
  "#4aa0ff",
  "#ffa24d",
  "#b78bff",
  "#3fe0b0",
  "#ff7aa8",
  "#5ad1e6",
];

export function hashString(text: string): number {
  let hash = 7;
  for (let i = 0; i < text.length; i++)
    hash = (hash * 31 + text.charCodeAt(i)) >>> 0;
  return hash;
}

export function projectColor(key: string): string {
  return ACCENTS[hashString(key) % ACCENTS.length] ?? "#4aa0ff";
}
