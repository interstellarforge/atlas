<script lang="ts">
  import { t } from "./i18n";
  import { htmlToMarkdown, renderMarkdown } from "./markdown";

  // Edits rich text but stores markdown: `value` in, `onchange` out. The text only changes
  // after the user edits, so opening an item never makes it look modified.
  let {
    value,
    labelledby,
    placeholder = "",
    onchange,
  }: {
    value: string;
    labelledby: string;
    placeholder?: string;
    onchange: (markdown: string) => void;
  } = $props();

  let el: HTMLDivElement;
  let last: string | undefined;
  let empty = $state(true);

  $effect(() => {
    if (value === last) return;
    last = value;
    // Safe: markdown-it runs with raw HTML off (see markdown.ts).
    // eslint-disable-next-line svelte/no-dom-manipulating -- contenteditable content cannot be Svelte-managed
    el.innerHTML = renderMarkdown(value);
    empty = value.trim() === "";
  });

  function input(): void {
    last = htmlToMarkdown(el.innerHTML);
    empty = el.textContent.trim() === "" && !el.querySelector("img,hr");
    onchange(last);
  }

  function run(command: string, arg?: string): void {
    el.focus();
    // eslint-disable-next-line @typescript-eslint/no-deprecated -- the only built-in rich text editing API
    document.execCommand(command, false, arg);
    input();
  }

  function link(): void {
    const url = window.prompt(t("item.editor.linkPrompt"), "https://");
    if (url && /^(https?:|mailto:)/i.test(url)) run("createLink", url);
  }

  // Plain text only: pasted markup never lands in the document.
  function paste(event: ClipboardEvent): void {
    event.preventDefault();
    // eslint-disable-next-line @typescript-eslint/no-deprecated -- the only built-in rich text editing API
    document.execCommand(
      "insertText",
      false,
      event.clipboardData?.getData("text/plain") ?? "",
    );
  }

  const tools = [
    {
      label: "item.editor.bold",
      glyph: "B",
      act: () => {
        run("bold");
      },
    },
    {
      label: "item.editor.italic",
      glyph: "I",
      act: () => {
        run("italic");
      },
    },
    {
      label: "item.editor.heading",
      glyph: "H",
      act: () => {
        run("formatBlock", "h3");
      },
    },
    {
      label: "item.editor.bullets",
      glyph: "•",
      act: () => {
        run("insertUnorderedList");
      },
    },
    {
      label: "item.editor.numbers",
      glyph: "1.",
      act: () => {
        run("insertOrderedList");
      },
    },
    {
      label: "item.editor.quote",
      glyph: "”",
      act: () => {
        run("formatBlock", "blockquote");
      },
    },
    {
      label: "item.editor.code",
      glyph: "</>",
      act: () => {
        run("formatBlock", "pre");
      },
    },
    { label: "item.editor.link", glyph: "↗", act: link },
  ] as const;
</script>

<div class="editor">
  <div class="toolbar" role="toolbar" aria-label={t("item.description")}>
    {#each tools as tool (tool.label)}
      <!-- mousedown keeps the text selection while a button is pressed -->
      <button
        type="button"
        title={t(tool.label)}
        aria-label={t(tool.label)}
        onmousedown={(e) => {
          e.preventDefault();
        }}
        onclick={tool.act}>{tool.glyph}</button
      >
    {/each}
  </div>
  <div class="surface">
    {#if empty}<span class="placeholder" aria-hidden="true">{placeholder}</span
      >{/if}
    <div
      bind:this={el}
      class="body"
      contenteditable="true"
      role="textbox"
      tabindex="0"
      aria-multiline="true"
      aria-labelledby={labelledby}
      oninput={input}
      onpaste={paste}
    ></div>
  </div>
</div>

<style>
  .editor {
    display: flex;
    flex-direction: column;
    min-height: 16rem;
    background: var(--color-bg);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-sm);
  }
  .editor:focus-within {
    border-color: var(--color-accent);
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    gap: 2px;
    padding: 4px;
    border-bottom: 1px solid var(--color-border);
    background: var(--color-soft);
  }
  .toolbar button {
    min-width: 32px;
    min-height: 32px;
    padding: 0 8px;
    border-color: transparent;
    background: transparent;
    font-weight: var(--atlas-fw-bold);
  }
  .surface {
    position: relative;
    flex: 1;
    display: flex;
  }
  .placeholder {
    position: absolute;
    inset: 10px 12px auto;
    color: var(--color-muted);
    pointer-events: none;
  }
  .body {
    flex: 1;
    min-width: 0;
    padding: 10px 12px;
    line-height: 1.55;
    outline: none;
    overflow-wrap: anywhere;
  }
  .body :global(> :first-child) {
    margin-top: 0;
  }
  .body :global(> :last-child) {
    margin-bottom: 0;
  }
  .body :global(h1),
  .body :global(h2),
  .body :global(h3) {
    margin: 1em 0 0.4em;
    line-height: 1.25;
  }
  .body :global(blockquote) {
    margin: 0.6em 0;
    padding-left: 12px;
    border-left: 3px solid var(--color-border);
    color: var(--color-muted);
  }
  .body :global(pre),
  .body :global(code) {
    font-family: var(--font-mono);
    font-size: 0.9em;
    background: var(--color-soft);
    border-radius: var(--atlas-radius-sm);
  }
  .body :global(pre) {
    padding: 8px 10px;
    overflow-x: auto;
  }
  .body :global(code) {
    padding: 1px 4px;
  }
  .body :global(table) {
    border-collapse: collapse;
  }
  .body :global(th),
  .body :global(td) {
    padding: 4px 8px;
    border: 1px solid var(--color-border);
  }
</style>
