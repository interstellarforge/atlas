<script lang="ts">
  import {
    ApiError,
    createToken,
    listTokens,
    revokeToken,
    type Token,
  } from "./api";
  import EmptyState from "./EmptyState.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import PageHero from "./PageHero.svelte";

  let { embedded = false }: { embedded?: boolean } = $props();

  let tokens = $state<Token[]>([]);
  let loaded = $state(false);
  let name = $state("");
  let write = $state(false);
  let projects = $state("");
  let secret = $state("");
  let error = $state("");
  let copied = $state(false);

  const dateFormat = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
  });

  async function load(): Promise<void> {
    tokens = await listTokens();
    loaded = true;
  }
  void load().catch(() => {
    error = t("tokens.loadFailed");
  });

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = "";
    copied = false;
    try {
      const made = await createToken({
        name,
        scopes: write ? ["read", "write"] : ["read"],
        projects: projects
          .split(",")
          .map((p) => p.trim().toUpperCase())
          .filter(Boolean),
      });
      secret = made.secret;
      name = "";
      projects = "";
      write = false;
      await load();
    } catch (e) {
      error = t("tokens.createFailed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }

  async function revoke(token: Token): Promise<void> {
    try {
      await revokeToken(token.id);
      await load();
    } catch {
      error = t("tokens.revokeFailed");
    }
  }

  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(secret);
      copied = true;
    } catch {
      /* clipboard blocked: the secret stays selectable on screen */
    }
  }
</script>

{#if !embedded}
  <PageHero title={t("tokens.title")} description={t("tokens.intro")} />
{/if}

<div class="grid">
  <section class="panel" aria-labelledby="tokens-list">
    <h2 id="tokens-list">{t("tokens.yours")}</h2>
    {#if loaded && tokens.length === 0}
      <EmptyState
        icon="key"
        title={t("tokens.empty")}
        text={t("tokens.emptyText")}
      />
    {:else}
      <ul class="list">
        {#each tokens as token (token.id)}
          <li class:revoked={Boolean(token.revoked_at)}>
            <span class="ic"><Icon name="key" /></span>
            <div class="info">
              <strong>{token.name}</strong>
              <span class="meta"
                ><code>{token.prefix}…</code> · {token.scopes.join(", ")} ·
                {token.projects && token.projects.length > 0
                  ? token.projects.join(", ")
                  : t("tokens.allProjects")} · {t("tokens.created", {
                  date: dateFormat.format(token.created_at),
                })}{token.last_used_at
                  ? ` · ${t("psettings.lastUsed", { date: dateFormat.format(token.last_used_at) })}`
                  : ` · ${t("psettings.neverUsed")}`}</span
              >
            </div>
            {#if token.revoked_at}
              <span class="chip warn">{t("tokens.revoked")}</span>
            {:else}
              <button type="button" onclick={() => void revoke(token)}
                >{t("tokens.revoke")}</button
              >
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  </section>

  <section class="panel" aria-labelledby="tokens-new">
    <h2 id="tokens-new">{t("tokens.new")}</h2>
    {#if secret}
      <div class="banner ok" role="status">
        <p>{t("tokens.secretOnce")}</p>
        <code class="secret">{secret}</code>
        <button type="button" onclick={() => void copy()}
          >{copied ? t("tokens.copied") : t("tokens.copy")}</button
        >
      </div>
    {/if}
    <form onsubmit={submit}>
      <label>
        {t("tokens.name")}
        <input bind:value={name} required maxlength="100" />
      </label>
      <label class="check">
        <input type="checkbox" bind:checked={write} />
        {t("tokens.allowWrite")}
      </label>
      <label>
        {t("tokens.projects")}
        <input bind:value={projects} placeholder="EXO, WEB" />
      </label>
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <button class="primary" type="submit">{t("tokens.create")}</button>
    </form>
  </section>
</div>

<style>
  .grid {
    display: grid;
    grid-template-columns: minmax(0, 1.5fr) minmax(0, 1fr);
    gap: var(--space-4);
    align-items: start;
  }
  .panel {
    display: grid;
    gap: var(--space-3);
    padding: 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
  }
  h2 {
    margin: 0;
    font-size: var(--atlas-fs-lg);
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
  }
  li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 12px 0;
    border-top: 1px solid var(--color-border);
  }
  li:first-child {
    border-top: none;
  }
  li.revoked {
    opacity: 0.6;
  }
  .ic {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    border-radius: var(--atlas-radius-sm);
    background: var(--color-accent-soft);
    color: var(--color-accent-soft-text);
  }
  .info {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: 2px;
  }
  .meta {
    color: var(--color-muted);
    font-size: var(--atlas-fs-sm);
    overflow-wrap: anywhere;
  }
  form {
    display: grid;
    gap: var(--space-3);
  }
  label {
    display: grid;
    gap: 6px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
  }
  label.check {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .banner p {
    margin: 0 0 var(--space-2);
  }
  .secret {
    display: block;
    margin-bottom: var(--space-2);
    padding: 8px 10px;
    border-radius: var(--atlas-radius-sm);
    background: var(--color-surface);
    color: var(--color-text);
    font-family: var(--font-mono);
    overflow-wrap: anywhere;
  }
  @media (max-width: 900px) {
    .grid {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
