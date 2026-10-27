<script lang="ts">
  import {
    ApiError,
    createProjectToken,
    deleteItemTemplate,
    importItems,
    type ImportReport,
    type ItemTemplate,
    listItemTemplates,
    listMembers,
    listProjectTokens,
    listRoles,
    type Member,
    type ProjectToken,
    removeMember,
    revokeProjectToken,
    type Role,
    saveItemTemplate,
    saveProjectTemplate,
    setMember,
    updateProject,
    type User,
    type Workflow,
  } from "./api";
  import { t } from "./i18n";
  import { people } from "./people.svelte";
  import { formatSubitems, parseSubitems } from "./subitems";
  import WorkflowSettings from "./WorkflowSettings.svelte";

  let {
    project,
    name,
    section,
    workflow,
    onchange,
    user,
  }: {
    user: User;
    project: string;
    name: string;
    section: string;
    workflow: Workflow;
    onchange: () => void;
  } = $props();

  const sections = [
    "general",
    "members",
    "workflow",
    "templates",
    "tokens",
    "data",
    "danger",
  ] as const;
  const active = $derived(
    (sections as readonly string[]).includes(section) ? section : "general",
  );
  const dateFormat = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
  });

  let notice = $state("");
  let error = $state("");
  let newName = $derived(name);

  async function run(
    done: string,
    action: () => Promise<unknown>,
  ): Promise<void> {
    notice = "";
    error = "";
    try {
      await action();
      notice = done;
    } catch (e) {
      error = t("settings.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }

  // ---- members ----
  let members = $state<Member[]>([]);
  let addUser = $state("");
  let addRole = $state("member");
  let roles = $state<Role[]>([]);
  function roleLabel(name: string): string {
    return ["admin", "member", "viewer"].includes(name)
      ? t(`psettings.roles.${name}` as "psettings.roles.admin")
      : name;
  }
  const candidates = $derived(
    people.users.filter((u) => !members.some((m) => m.user_id === u.id)),
  );
  async function loadMembers(): Promise<void> {
    [members, roles] = await Promise.all([
      listMembers(project),
      listRoles().then((r) => r.items),
    ]);
  }

  // ---- tokens ----
  let tokens = $state<ProjectToken[]>([]);
  let tokenName = $state("");
  let tokenWrite = $state(false);
  let tokenExpiry = $state("");
  let secret = $state("");
  async function loadTokens(): Promise<void> {
    tokens = await listProjectTokens(project);
  }

  // ---- templates ----
  const blank = {
    name: "",
    type: "task",
    title_prefix: "",
    description: "",
    priority: "normal",
    labels: "",
    status: "",
    checklist: "",
    subitems: "",
  };
  const subitemsExample = "story: Plan\n  task: Draft\n  task: Review";
  let templates = $state<ItemTemplate[]>([]);
  let draft = $state({ ...blank });
  let asTemplate = $state("");
  async function loadTemplates(): Promise<void> {
    templates = await listItemTemplates(project);
  }
  function lines(text: string, sep: RegExp): string[] {
    return text
      .split(sep)
      .map((x) => x.trim())
      .filter(Boolean);
  }

  // ---- import ----
  let report = $state<ImportReport | undefined>();
  let file = $state<{ text: string; format: "json" | "csv" } | undefined>();

  $effect(() => {
    if (active === "members") void loadMembers();
    if (active === "tokens") void loadTokens();
    if (active === "templates") void loadTemplates();
  });

  async function pickFile(event: Event): Promise<void> {
    const f = (event.currentTarget as HTMLInputElement).files?.[0];
    report = undefined;
    if (!f) {
      file = undefined;
      return;
    }
    file = {
      text: await f.text(),
      format: f.name.toLowerCase().endsWith(".csv") ? "csv" : "json",
    };
  }

  function runImport(dryRun: boolean): void {
    const chosen = file;
    if (!chosen) return;
    void run(
      dryRun ? t("psettings.dryRunDone") : t("psettings.importDone"),
      async () => {
        report = await importItems(project, chosen.text, chosen.format, dryRun);
        if (!dryRun && report.applied) onchange();
      },
    );
  }
</script>

<div class="layout">
  <nav class="subnav" aria-label={t("psettings.sections")}>
    {#each sections as s (s)}
      <a
        href="#/p/{project}/settings/{s}"
        aria-current={active === s ? "page" : undefined}
        >{t(`psettings.nav.${s}` as "psettings.nav.general")}</a
      >
    {/each}
  </nav>

  <div class="content">
    {#if notice}<p class="banner ok" role="status">{notice}</p>{/if}
    {#if error}<p class="banner error" role="alert">{error}</p>{/if}

    {#if active === "general"}
      <form
        class="panel"
        onsubmit={(e) => {
          e.preventDefault();
          void run(t("settings.saved"), async () => {
            await updateProject(project, { name: newName });
            onchange();
          });
        }}
      >
        <h2>{t("psettings.nav.general")}</h2>
        <label>
          {t("projects.key")}
          <input value={project} readonly />
          <small class="muted">{t("psettings.keyHint")}</small>
        </label>
        <label>
          {t("projects.name")}
          <input bind:value={newName} required maxlength="100" />
        </label>
        <div>
          <button class="primary" type="submit">{t("settings.save")}</button>
        </div>
      </form>
    {:else if active === "members"}
      <section class="panel" aria-labelledby="members-title">
        <h2 id="members-title">{t("psettings.nav.members")}</h2>
        <ul class="list">
          {#each members as m (m.user_id)}
            <li>
              <div>
                <strong>{m.name}</strong>
                <span class="muted">{m.email}</span>
              </div>
              <select
                aria-label={t("psettings.roleOf", { name: m.name })}
                value={m.role}
                onchange={(e) =>
                  void run(t("settings.saved"), async () => {
                    await setMember(project, m.user_id, e.currentTarget.value);
                    await loadMembers();
                  })}
              >
                {#each roles as r (r.name)}
                  <option value={r.name}>{roleLabel(r.name)}</option>
                {/each}
              </select>
              <button
                type="button"
                onclick={() =>
                  void run(t("psettings.memberRemoved"), async () => {
                    await removeMember(project, m.user_id);
                    await loadMembers();
                  })}>{t("psettings.remove")}</button
              >
            </li>
          {/each}
        </ul>
        {#if candidates.length > 0}
          <form
            class="inline"
            onsubmit={(e) => {
              e.preventDefault();
              void run(t("settings.saved"), async () => {
                await setMember(project, Number(addUser), addRole);
                addUser = "";
                await loadMembers();
              });
            }}
          >
            <select
              aria-label={t("psettings.addMember")}
              bind:value={addUser}
              required
            >
              <option value="">{t("psettings.chooseUser")}</option>
              {#each candidates as u (u.id)}
                <option value={String(u.id)}>{u.name}</option>
              {/each}
            </select>
            <select aria-label={t("psettings.role")} bind:value={addRole}>
              {#each roles as r (r.name)}
                <option value={r.name}>{roleLabel(r.name)}</option>
              {/each}
            </select>
            <button class="primary" type="submit"
              >{t("psettings.addMember")}</button
            >
          </form>
        {/if}
      </section>
    {:else if active === "workflow"}
      <WorkflowSettings {project} {workflow} />
    {:else if active === "templates"}
      <section class="panel" aria-labelledby="tpl-title">
        <h2 id="tpl-title">{t("psettings.nav.templates")}</h2>
        <p class="muted hint">{t("psettings.templatesIntro")}</p>
        <ul class="list">
          {#each templates as tpl (tpl.id)}
            <li>
              <div>
                <strong>{tpl.name}</strong>
                <span class="muted"
                  >{t(`types.${tpl.type}` as "types.epic")}{tpl.scope ===
                  "instance"
                    ? ` · ${t("psettings.instanceWide")}`
                    : ""}{tpl.status ? ` · ${tpl.status}` : ""}{tpl.subitems
                    .length > 0
                    ? ` · ${t("psettings.subitemCount", { count: formatSubitems(tpl.subitems).split("\n").length })}`
                    : ""}{tpl.checklist.length > 0
                    ? ` · ${t("psettings.checklistCount", { count: tpl.checklist.length })}`
                    : ""}</span
                >
              </div>
              <button
                type="button"
                disabled={tpl.scope === "instance"}
                onclick={() =>
                  void run(t("psettings.templateDeleted"), async () => {
                    await deleteItemTemplate(project, tpl.id);
                    await loadTemplates();
                  })}>{t("psettings.remove")}</button
              >
            </li>
          {/each}
        </ul>
        <form
          class="stack"
          onsubmit={(e) => {
            e.preventDefault();
            void run(t("psettings.templateSaved"), async () => {
              await saveItemTemplate(project, {
                name: draft.name,
                type: draft.type,
                title_prefix: draft.title_prefix,
                description: draft.description,
                priority: draft.priority,
                labels: lines(draft.labels, /,/),
                status: draft.status,
                checklist: lines(draft.checklist, /\n/),
                subitems: parseSubitems(draft.subitems),
              });
              draft = { ...blank };
              await loadTemplates();
            });
          }}
        >
          <h3>{t("psettings.newTemplate")}</h3>
          <label
            >{t("psettings.templateName")}<input
              bind:value={draft.name}
              required
              maxlength="60"
            /></label
          >
          <div class="inline">
            <label>
              {t("board.type")}
              <select bind:value={draft.type}>
                {#each workflow.types as typ (typ)}
                  <option value={typ}
                    >{t(`types.${typ}` as "types.epic")}</option
                  >
                {/each}
              </select>
            </label>
            <label>
              {t("item.priority")}
              <select bind:value={draft.priority}>
                {#each ["low", "normal", "high", "urgent"] as p (p)}
                  <option value={p}
                    >{t(`priority.${p}` as "priority.low")}</option
                  >
                {/each}
              </select>
            </label>
            <label>
              {t("board.status")}
              <select bind:value={draft.status}>
                <option value="">{t("psettings.defaultStatus")}</option>
                {#each workflow.statuses.filter((st) => st.allow_create) as st (st.id)}
                  <option value={st.name}>{st.name}</option>
                {/each}
              </select>
            </label>
          </div>
          <label
            >{t("psettings.titlePrefix")}<input
              bind:value={draft.title_prefix}
              maxlength="100"
            /></label
          >
          <label
            >{t("item.description")}<textarea
              bind:value={draft.description}
              rows="4"></textarea></label
          >
          <label
            >{t("item.labels")}<input
              bind:value={draft.labels}
              placeholder={t("item.labelsHint")}
            /></label
          >
          <label
            >{t("psettings.checklistLines")}<textarea
              bind:value={draft.checklist}
              rows="3"></textarea></label
          >
          <label
            >{t("psettings.subitems")}<textarea
              bind:value={draft.subitems}
              rows="4"
              placeholder={subitemsExample}></textarea></label
          >
          <p class="muted hint">{t("psettings.subitemsHint")}</p>
          <p class="muted hint">{t("psettings.placeholders")}</p>
          <div>
            <button class="primary" type="submit"
              >{t("psettings.saveTemplate")}</button
            >
          </div>
        </form>
      </section>
      {#if user.is_admin}
        <form
          class="panel"
          onsubmit={(e) => {
            e.preventDefault();
            void run(t("psettings.projectTemplateSaved"), async () => {
              await saveProjectTemplate({
                name: asTemplate,
                from_project: project,
              });
              asTemplate = "";
            });
          }}
        >
          <h2>{t("psettings.saveAsProjectTemplate")}</h2>
          <p class="muted hint">{t("psettings.projectTemplateHint")}</p>
          <label
            >{t("psettings.templateName")}<input
              bind:value={asTemplate}
              required
              maxlength="60"
            /></label
          >
          <div>
            <button class="primary" type="submit"
              >{t("psettings.saveTemplate")}</button
            >
          </div>
        </form>
      {/if}
    {:else if active === "tokens"}
      <section class="panel" aria-labelledby="ptokens-title">
        <h2 id="ptokens-title">{t("psettings.nav.tokens")}</h2>
        <p class="muted hint">{t("psettings.tokensIntro")}</p>
        {#if secret}
          <div class="banner ok" role="status">
            <p>{t("tokens.secretOnce")}</p>
            <code class="secret">{secret}</code>
          </div>
        {/if}
        <ul class="list">
          {#each tokens as tok (tok.id)}
            <li class:revoked={Boolean(tok.revoked_at)}>
              <div>
                <strong>{tok.name}</strong>
                <span class="muted"
                  ><code>{tok.prefix}…</code> · {tok.scopes.join(", ")} · {t(
                    "psettings.owner",
                    { name: tok.owner },
                  )} ·
                  {tok.last_used_at
                    ? t("psettings.lastUsed", {
                        date: dateFormat.format(tok.last_used_at),
                      })
                    : t("psettings.neverUsed")}{tok.expires_at
                    ? ` · ${t("psettings.expires", { date: dateFormat.format(tok.expires_at) })}`
                    : ""}</span
                >
              </div>
              {#if tok.revoked_at}
                <span class="chip warn">{t("tokens.revoked")}</span>
              {:else}
                <button
                  type="button"
                  onclick={() =>
                    void run(t("psettings.tokenRevoked"), async () => {
                      await revokeProjectToken(project, tok.id);
                      await loadTokens();
                    })}>{t("tokens.revoke")}</button
                >
              {/if}
            </li>
          {/each}
        </ul>
        <form
          class="stack"
          onsubmit={(e) => {
            e.preventDefault();
            void run(t("psettings.tokenCreated"), async () => {
              const made = await createProjectToken(project, {
                name: tokenName,
                write: tokenWrite,
                expires_at: tokenExpiry
                  ? new Date(tokenExpiry).getTime()
                  : null,
              });
              secret = made.secret;
              tokenName = "";
              tokenWrite = false;
              tokenExpiry = "";
              await loadTokens();
            });
          }}
        >
          <label>
            {t("tokens.name")}
            <input bind:value={tokenName} required maxlength="100" />
          </label>
          <label>
            {t("psettings.expiry")}
            <input type="date" bind:value={tokenExpiry} />
          </label>
          <label class="check">
            <input type="checkbox" bind:checked={tokenWrite} />
            {t("tokens.allowWrite")}
          </label>
          <div>
            <button class="primary" type="submit">{t("tokens.create")}</button>
          </div>
        </form>
      </section>
    {:else if active === "data"}
      <section class="panel" aria-labelledby="data-title">
        <h2 id="data-title">{t("psettings.nav.data")}</h2>
        <p class="muted hint">{t("psettings.exportIntro")}</p>
        <p class="row">
          <a
            class="btn"
            href="/api/v1/projects/{project}/export"
            download="{project}.json">{t("psettings.exportJson")}</a
          >
          <a
            class="btn"
            href="/api/v1/projects/{project}/export?format=csv"
            download="{project}.csv">{t("psettings.exportCsv")}</a
          >
        </p>
        <h3>{t("psettings.import")}</h3>
        <p class="muted hint">{t("psettings.importIntro")}</p>
        <input
          type="file"
          accept=".json,.csv"
          aria-label={t("psettings.import")}
          onchange={(e) => void pickFile(e)}
        />
        <div class="row">
          <button
            type="button"
            disabled={!file}
            onclick={() => {
              runImport(true);
            }}>{t("psettings.dryRun")}</button
          >
          <button
            class="primary"
            type="button"
            disabled={!file}
            onclick={() => {
              runImport(false);
            }}>{t("psettings.runImport")}</button
          >
        </div>
        {#if report}
          <div class="report" role="status">
            <p>
              <strong
                >{report.applied
                  ? t("psettings.applied")
                  : t("psettings.notApplied")}</strong
              >
              · {t("psettings.created", { count: report.created })} · {t(
                "psettings.duplicates",
                { count: report.duplicates.length },
              )} · {t("psettings.errors", { count: report.errors.length })}
            </p>
            {#if report.errors.length > 0}
              <ul>
                {#each report.errors.slice(0, 20) as err (err.row)}
                  <li>{t("psettings.row", { row: err.row })}: {err.message}</li>
                {/each}
              </ul>
            {/if}
          </div>
        {/if}
      </section>
    {:else}
      <section class="panel danger" aria-labelledby="danger-title">
        <h2 id="danger-title">{t("psettings.nav.danger")}</h2>
        <p class="muted hint">{t("psettings.archiveHint")}</p>
        <div>
          <button
            type="button"
            onclick={() =>
              void run(t("settings.saved"), async () => {
                await updateProject(project, { archived: true });
                onchange();
              })}>{t("psettings.archive")}</button
          >
        </div>
      </section>
    {/if}
  </div>
</div>

<style>
  .layout {
    display: grid;
    grid-template-columns: 200px minmax(0, 1fr);
    gap: var(--space-4);
    align-items: start;
    overflow-y: auto;
    max-height: 100%;
  }
  .subnav {
    display: grid;
    gap: 2px;
    padding: var(--space-2);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
  }
  .subnav a {
    padding: 9px 12px;
    border-radius: var(--atlas-radius-sm);
    color: var(--color-muted);
    font-weight: var(--atlas-fw-semibold);
    text-decoration: none;
  }
  .subnav a:hover {
    background: var(--color-soft);
  }
  .subnav a[aria-current="page"] {
    background: var(--color-accent-soft);
    color: var(--color-accent-soft-text);
  }
  .content {
    display: grid;
    gap: var(--space-3);
    min-width: 0;
  }
  .panel {
    display: grid;
    gap: var(--space-3);
    padding: 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
  }
  .panel.danger {
    border-color: var(--color-danger);
  }
  h2,
  h3 {
    margin: 0;
  }
  h2 {
    font-size: var(--atlas-fs-lg);
  }
  h3 {
    font-size: var(--atlas-fs-md);
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
  .hint {
    margin: 0;
    font-size: var(--atlas-fs-sm);
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
  }
  .list li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 10px 0;
    border-top: 1px solid var(--color-border);
  }
  .list li:first-child {
    border-top: none;
  }
  .list li.revoked {
    opacity: 0.6;
  }
  .list li > div {
    flex: 1;
    min-width: 0;
    display: grid;
    gap: 2px;
    overflow-wrap: anywhere;
  }
  .inline,
  .row {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    align-items: center;
    margin: 0;
  }
  .stack {
    display: grid;
    gap: var(--space-3);
  }
  .btn {
    display: inline-flex;
    align-items: center;
    min-height: 38px;
    padding: 0 14px;
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-surface);
    color: var(--color-text);
    font-weight: var(--atlas-fw-semibold);
    text-decoration: none;
  }
  .secret {
    display: block;
    padding: 8px 10px;
    border-radius: var(--atlas-radius-sm);
    background: var(--color-surface);
    color: var(--color-text);
    font-family: var(--font-mono);
    overflow-wrap: anywhere;
  }
  .banner p {
    margin: 0 0 var(--space-2);
  }
  .report ul {
    margin: var(--space-2) 0 0;
    padding-left: 18px;
  }
  @media (max-width: 800px) {
    .layout {
      grid-template-columns: minmax(0, 1fr);
    }
    .subnav {
      display: flex;
      overflow-x: auto;
    }
  }
</style>
