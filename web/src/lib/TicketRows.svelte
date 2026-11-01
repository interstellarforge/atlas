<script lang="ts">
  import type { Card, Workflow } from "./api";
  import { t } from "./i18n";
  import { itemHref } from "./routes";
  import StatusChip from "./StatusChip.svelte";
  import { statusTone } from "./statusTone";
  import TypeIcon from "./TypeIcon.svelte";

  let {
    items,
    workflow,
    indent = false,
  }: {
    items: Card[];
    workflow: Workflow | undefined;
    indent?: boolean;
  } = $props();

  const date = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });

  function tone(name: string | undefined): ReturnType<typeof statusTone> {
    const all = workflow?.statuses ?? [];
    const st = all.find((x) => x.name === name);
    return st ? statusTone(st, all) : "todo";
  }
</script>

<ul class="tickets" class:indent>
  {#each items as it (it.key)}
    <li>
      <TypeIcon type={it.type} size={14} />
      <a href={itemHref(it.key)}><span class="key">{it.key}</span> {it.title}</a
      >
      <StatusChip name={it.status ?? ""} tone={tone(it.status)} />
      <span class="muted"
        >{it.due_at === null
          ? t("tickets.unscheduled")
          : t("timeline.due", { date: date.format(it.due_at) })}</span
      >
    </li>
  {/each}
</ul>

<style>
  .tickets {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .indent {
    padding-left: var(--space-4);
  }
  li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    min-height: 40px;
    padding: 6px var(--space-3);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
  }
  a {
    flex: 1 1 200px;
    min-width: 0;
    color: inherit;
    text-decoration: none;
    overflow-wrap: anywhere;
  }
  a:hover {
    text-decoration: underline;
  }
  a:focus-visible {
    outline: 2px solid var(--atlas-primary);
    outline-offset: 2px;
  }
  .key {
    color: var(--color-muted);
    font-weight: var(--atlas-fw-semibold);
  }
  .muted {
    color: var(--color-muted);
    font-size: var(--atlas-fs-sm);
  }
</style>
