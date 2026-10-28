import MarkdownIt from "markdown-it";
import TurndownService from "turndown";
import { gfm } from "turndown-plugin-gfm";

// Raw HTML stays off: the output only contains tags markdown-it generates itself, and it refuses
// javascript: links, so the result is safe to insert as HTML without a sanitizer.
const md = new MarkdownIt({ html: false, linkify: false, breaks: true });

const defaultLink =
  md.renderer.rules["link_open"] ??
  ((tokens, idx, options, _env, self) =>
    self.renderToken(tokens, idx, options));
md.renderer.rules["link_open"] = (tokens, idx, options, env, self) => {
  tokens[idx]?.attrSet("rel", "noopener noreferrer");
  tokens[idx]?.attrSet("target", "_blank");
  return defaultLink(tokens, idx, options, env, self);
};

export function renderMarkdown(text: string): string {
  return md.render(text);
}

const td = new TurndownService({
  headingStyle: "atx",
  bulletListMarker: "-",
  codeBlockStyle: "fenced",
});
td.use(gfm);

// Inverse of renderMarkdown, for the rich text editor.
export function htmlToMarkdown(html: string): string {
  return td.turndown(html);
}
