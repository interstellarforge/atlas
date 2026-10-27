import en from "../locales/en.json";

type Messages = typeof en;
type Paths<T, P extends string = ""> = {
  [K in keyof T & string]: T[K] extends string
    ? `${P}${K}`
    : Paths<T[K], `${P}${K}.`>;
}[keyof T & string];

export type MessageKey = Paths<Messages>;

export function t(
  key: MessageKey,
  params: Record<string, string | number> = {},
): string {
  let node: unknown = en;
  for (const part of key.split(".")) {
    node = (node as Record<string, unknown>)[part];
  }
  const text = typeof node === "string" ? node : key;
  return text.replace(/\{(\w+)\}/g, (_, name: string) =>
    String(params[name] ?? `{${name}}`),
  );
}
