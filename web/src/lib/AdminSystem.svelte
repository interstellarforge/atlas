<script lang="ts">
  import {
    ApiError,
    getLogs,
    getMcpStats,
    type LogEntry,
    type McpStats,
  } from "./api";
  import { t } from "./i18n";

  // Administration: who is connected over MCP, and the recent server log (ADR-0016). Both are in-memory
  // windows kept by the server; they refresh themselves while the page is open.
  let { kind }: { kind: "mcp" | "logs" } = $props();

  const LOG_SIZE = 200;
  const REFRESH_MS = 10_000;

  let stats = $state<McpStats | undefined>();
  let logs = $state<LogEntry[]>([]);
  let level = $state("info");
  let q = $state("");
  let error = $state("");

  const time = new Intl.DateTimeFormat(undefined, {
    dateStyle: "short",
    timeStyle: "medium",
  });

  async function load(which = kind): Promise<void> {
    try {
      if (which === "mcp") stats = await getMcpStats();
      else logs = await getLogs({ level, q: q.trim() });
      error = "";
    } catch (e) {
      error = e instanceof ApiError ? e.message : t("admin.loadFailed");
    }
  }

  $effect(() => {
    const which = kind;
    void load(which);
    const id = setInterval(() => void load(which), REFRESH_MS);
    return () => {
      clearInterval(id);
    };
  });

  function attrs(e: LogEntry): string {
    return Object.entries(e.attrs ?? {})
      .map(([k, v]) => `${k}=${v}`)
      .join(" ");
  }
  function bytes(n: number): string {
    return n < 1024 ? `${String(n)} B` : `${(n / 1024).toFixed(1)} KiB`;
  }
</script>

{#if error}<p class="banner error" role="alert">{error}</p>{/if}

{#if kind === "mcp"}
  <section class="panel" aria-labelledby="mcp-title">
    <h2 id="mcp-title">{t("admin.nav.mcp")}</h2>
    <p class="muted hint">{t("admin.system.mcpIntro")}</p>
    {#if stats}
      <p class="summary">
        <span class="pill ok">{t("admin.system.statusOk")}</span>
        <span class="pill" class:ok={stats.connected > 0}
          >{t("admin.system.connected", { count: stats.connected })}</span
        >
        <span class="pill"
          >{t("admin.system.totalCalls", { count: stats.calls })}</span
        >
        <span class="muted"
          >{t("admin.system.uptime", {
            date: time.format(stats.started_at),
          })}</span
        >
      </p>

      <h3>{t("admin.system.clients")}</h3>
      {#if stats.clients.length === 0}
        <p class="muted">{t("admin.system.noClients")}</p>
      {:else}
        <div class="scroll">
          <table>
            <thead>
              <tr>
                <th scope="col">{t("admin.system.status")}</th>
                <th scope="col">{t("admin.system.client")}</th>
                <th scope="col">{t("admin.system.token")}</th>
                <th scope="col">{t("admin.system.lastSeen")}</th>
                <th scope="col">{t("admin.system.requests")}</th>
                <th scope="col">{t("admin.system.calls")}</th>
                <th scope="col">{t("admin.system.errors")}</th>
                <th scope="col">{t("admin.system.lastTool")}</th>
              </tr>
            </thead>
            <tbody>
              {#each stats.clients as c (c.actor)}
                <tr>
                  <td
                    ><span class="pill" class:ok={c.connected}
                      >{c.connected
                        ? t("admin.system.online")
                        : t("admin.system.idle")}</span
                    ></td
                  >
                  <td title={c.user_agent}>{c.name ?? c.user_agent ?? "—"}</td>
                  <td>{c.label ?? ""} <code>{c.actor}</code></td>
                  <td>{time.format(c.last_seen)}</td>
                  <td>{c.requests}</td>
                  <td>{c.calls}</td>
                  <td>{c.errors}</td>
                  <td>{c.last_tool ?? ""}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}

      <h3>{t("admin.system.recent")}</h3>
      {#if stats.recent.length === 0}
        <p class="muted">{t("admin.system.noCalls")}</p>
      {:else}
        <div class="scroll">
          <table>
            <thead>
              <tr>
                <th scope="col">{t("admin.when")}</th>
                <th scope="col">{t("admin.actor")}</th>
                <th scope="col">{t("admin.system.tool")}</th>
                <th scope="col">{t("admin.system.duration")}</th>
                <th scope="col">{t("admin.system.size")}</th>
                <th scope="col">{t("admin.system.result")}</th>
              </tr>
            </thead>
            <tbody>
              {#each stats.recent.slice(0, 50) as c, i (i)}
                <tr>
                  <td>{time.format(c.ts)}</td>
                  <td><code>{c.actor}</code></td>
                  <td>{c.tool}</td>
                  <td>{c.ms} ms</td>
                  <td>{bytes(c.bytes)}</td>
                  <td class:bad={c.error}
                    >{c.error ?? t("admin.system.success")}</td
                  >
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {/if}
  </section>
{:else}
  <section class="panel" aria-labelledby="log-title">
    <h2 id="log-title">{t("admin.nav.logs")}</h2>
    <p class="muted hint">{t("admin.system.logIntro", { count: 1000 })}</p>
    <form
      class="inline"
      onsubmit={(e) => {
        e.preventDefault();
        void load();
      }}
    >
      <select
        aria-label={t("admin.system.level")}
        bind:value={level}
        onchange={() => void load()}
      >
        {#each ["debug", "info", "warn", "error"] as l (l)}
          <option value={l}
            >{t(
              `admin.system.levels.${l}` as "admin.system.levels.info",
            )}</option
          >
        {/each}
      </select>
      <input
        aria-label={t("admin.system.search")}
        placeholder={t("admin.system.search")}
        bind:value={q}
      />
      <button class="primary" type="submit">{t("admin.filter")}</button>
      <button type="button" onclick={() => void load()}
        >{t("admin.system.refresh")}</button
      >
    </form>
    {#if logs.length === 0}
      <p class="muted">{t("admin.system.logEmpty")}</p>
    {:else}
      <ol class="log">
        {#each logs as e, i (i)}
          <li class={e.level.toLowerCase()}>
            <time>{time.format(e.ts)}</time>
            <b>{e.level}</b>
            <span>{e.msg}</span>
            <code>{attrs(e)}</code>
          </li>
        {/each}
      </ol>
      {#if logs.length >= LOG_SIZE}<p class="muted">{String(LOG_SIZE)}+</p>{/if}
    {/if}
  </section>
{/if}

<style>
  .panel {
    display: grid;
    gap: var(--space-3);
    padding: 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
  }
  h2,
  h3 {
    margin: 0;
    font-size: var(--atlas-fs-lg);
  }
  h3 {
    font-size: var(--atlas-fs-md, 1rem);
  }
  .summary {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
  }
  .pill {
    padding: 2px 10px;
    border: 1px solid var(--color-border);
    border-radius: 999px;
    font-size: var(--atlas-fs-sm);
  }
  .pill.ok {
    border-color: var(--atlas-st-done);
    color: var(--atlas-st-done);
  }
  .bad {
    color: var(--atlas-danger);
    font-weight: var(--atlas-fw-semibold);
  }
  .inline {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .scroll {
    overflow-x: auto;
  }
  table {
    width: 100%;
    border-collapse: collapse;
  }
  th,
  td {
    padding: 6px 10px;
    text-align: left;
    white-space: nowrap;
    border-bottom: 1px solid var(--color-border);
  }
  .log {
    display: grid;
    gap: 2px;
    margin: 0;
    padding: 0;
    list-style: none;
    font-family: var(--atlas-font-mono, ui-monospace, monospace);
    font-size: var(--atlas-fs-sm);
  }
  .log li {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
    padding: 3px 0;
    border-bottom: 1px solid var(--color-border);
  }
  .log time,
  .log code {
    color: var(--color-muted);
  }
  .log code {
    overflow-wrap: anywhere;
  }
  .log .warn b {
    color: var(--atlas-st-review);
  }
  .log .error b {
    color: var(--atlas-danger);
  }
</style>
