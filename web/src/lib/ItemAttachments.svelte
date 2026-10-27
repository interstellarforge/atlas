<script lang="ts">
  import { onMount } from "svelte";

  import {
    ApiError,
    type Attachment,
    getAttachments,
    uploadAttachment,
  } from "./api";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";

  let { itemKey }: { itemKey: string } = $props();

  let items = $state<Attachment[]>([]);
  let error = $state("");
  let uploading = $state(false);

  async function load(): Promise<void> {
    try {
      items = await getAttachments(itemKey);
    } catch {
      /* the panel reports a failed load; the next event retries */
    }
  }

  async function upload(files: Iterable<File>): Promise<void> {
    error = "";
    uploading = true;
    try {
      for (const file of files) await uploadAttachment(itemKey, file);
      await load();
    } catch (e) {
      error =
        e instanceof ApiError && e.status === 413
          ? t("item.attachmentTooLarge")
          : t("item.attachmentFailed", {
              message: e instanceof Error ? e.message : "",
            });
    } finally {
      uploading = false;
    }
  }

  function picked(event: Event & { currentTarget: HTMLInputElement }): void {
    const input = event.currentTarget;
    void upload(Array.from(input.files ?? [])).then(() => {
      input.value = "";
    });
  }

  // Pasting a screenshot anywhere in the panel attaches it.
  function pasted(event: ClipboardEvent): void {
    const files = Array.from(event.clipboardData?.files ?? []).filter((f) =>
      f.type.startsWith("image/"),
    );
    if (files.length === 0) return;
    event.preventDefault();
    void upload(files);
  }

  onMount(() => {
    void load();
    const offChange = onChange((event) => {
      if (event.key === itemKey && event.type === "attachment.added")
        void load();
    });
    const offResync = onResync(() => void load());
    document.addEventListener("paste", pasted);
    return () => {
      offChange();
      offResync();
      document.removeEventListener("paste", pasted);
    };
  });
</script>

<section aria-labelledby="item-attachments">
  <h3 id="item-attachments">{t("item.attachments")}</h3>
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if items.length === 0}
    <p class="meta">{t("item.noAttachments")}</p>
  {:else}
    <ul>
      {#each items as a (a.id)}
        <li>
          <a
            href={a.url}
            target="_blank"
            rel="noopener noreferrer"
            aria-label={t("item.attachmentOpen", { name: a.filename })}
          >
            <img
              src={a.thumb_url}
              alt={a.filename}
              loading="lazy"
              width="120"
              height="90"
            />
          </a>
          <span class="meta"
            >{a.filename} · {t("item.attachmentDims", {
              width: a.width,
              height: a.height,
            })}</span
          >
        </li>
      {/each}
    </ul>
  {/if}
  <label>
    {t("item.attachmentAdd")}
    <input
      type="file"
      accept="image/png,image/jpeg,image/webp"
      multiple
      disabled={uploading}
      onchange={picked}
    />
  </label>
</section>

<style>
  section {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  h3 {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--atlas-fs-md);
    font-weight: var(--atlas-fw-bold);
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
  li {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    max-width: 120px;
  }
  img {
    display: block;
    object-fit: cover;
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-bg);
  }
  label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: 0.8rem;
    color: var(--color-muted);
  }
  .meta {
    margin: 0;
    font-size: 0.7rem;
    color: var(--color-muted);
    overflow-wrap: anywhere;
  }
</style>
