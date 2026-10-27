<script lang="ts">
  import {
    type AdminUser,
    ApiError,
    type AuditEntry,
    createUser,
    deleteProjectTemplate,
    getAudit,
    getInstance,
    listAdminUsers,
    listProjectTemplates,
    patchAdminUser,
    type ProjectTemplate,
    type RightRow,
    saveProjectTemplate,
    type User,
  } from "./api";
  import { t } from "./i18n";
  import PageHero from "./PageHero.svelte";

  let { user, section }: { user: User; section: string } = $props();

  const sections = [
    "users",
    "templates",
    "rights",
    "audit",
    "instance",
  ] as const;
  const active = $derived(
    (sections as readonly string[]).includes(section) ? section : "users",
  );
  const dateTime = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });

  let templates = $state<ProjectTemplate[]>([]);
  let importText = $state("");
  let importName = $state("");
  let users = $state<AdminUser[]>([]);
  let rights = $state<RightRow[]>([]);
  let settings = $state<Record<string, string | number | boolean>>({});
  let audit = $state<AuditEntry[]>([]);
  let notice = $state("");
  let error = $state("");
  let email = $state("");
  let name = $state("");
  let password = $state("");
  let isAdmin = $state(false);
  let fActor = $state("");
  let fOperation = $state("");
  let fTarget = $state("");

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

  async function loadUsers(): Promise<void> {
    users = await listAdminUsers();
  }

  async function loadAudit(more = false): Promise<void> {
    const before = more ? audit[audit.length - 1]?.id : undefined;
    const page = await getAudit({
      actor: fActor.trim(),
      operation: fOperation.trim(),
      target: fTarget.trim(),
      ...(before !== undefined && { before }),
    });
    audit = more ? [...audit, ...page] : page;
  }

  $effect(() => {
    if (active === "users")
      void loadUsers().catch(() => (error = t("admin.loadFailed")));
    if (active === "templates")
      void listProjectTemplates()
        .then((list) => (templates = list))
        .catch(() => (error = t("admin.loadFailed")));
    if (active === "audit")
      void loadAudit().catch(() => (error = t("admin.loadFailed")));
    if (active === "rights" || active === "instance")
      void getInstance()
        .then((i) => {
          rights = i.rights;
          settings = i.settings;
        })
        .catch(() => (error = t("admin.loadFailed")));
  });
</script>

<PageHero title={t("admin.title")} description={t("admin.intro")} />

<nav class="subnav" aria-label={t("admin.sections")}>
  {#each sections as s (s)}
    <a href="#/admin/{s}" aria-current={active === s ? "page" : undefined}
      >{t(`admin.nav.${s}` as "admin.nav.users")}</a
    >
  {/each}
</nav>

{#if notice}<p class="banner ok" role="status">{notice}</p>{/if}
{#if error}<p class="banner error" role="alert">{error}</p>{/if}

{#if active === "users"}
  <section class="panel" aria-labelledby="users-title">
    <h2 id="users-title">{t("admin.nav.users")}</h2>
    <ul class="list">
      {#each users as u (u.id)}
        <li class:off={Boolean(u.deactivated_at)}>
          <div>
            <strong>{u.name}</strong>
            <span class="muted">{u.email}</span>
          </div>
          {#if u.deactivated_at}<span class="chip warn"
              >{t("admin.deactivated")}</span
            >{/if}
          {#if u.is_admin}<span class="chip type">{t("admin.admin")}</span>{/if}
          <button
            type="button"
            onclick={() =>
              void run(t("settings.saved"), async () => {
                await patchAdminUser(u.id, { is_admin: !u.is_admin });
                await loadUsers();
              })}
            >{u.is_admin
              ? t("admin.removeAdmin")
              : t("admin.makeAdmin")}</button
          >
          <button
            type="button"
            disabled={u.id === user.id}
            onclick={() =>
              void run(t("settings.saved"), async () => {
                await patchAdminUser(u.id, {
                  active: Boolean(u.deactivated_at),
                });
                await loadUsers();
              })}
            >{u.deactivated_at
              ? t("admin.activate")
              : t("admin.deactivate")}</button
          >
        </li>
      {/each}
    </ul>
  </section>
  <form
    class="panel"
    onsubmit={(e) => {
      e.preventDefault();
      void run(t("admin.userCreated"), async () => {
        await createUser({ email, name, password, is_admin: isAdmin });
        email = "";
        name = "";
        password = "";
        isAdmin = false;
        await loadUsers();
      });
    }}
  >
    <h2>{t("admin.newUser")}</h2>
    <label
      >{t("settings.email")}<input
        type="email"
        bind:value={email}
        required
      /></label
    >
    <label
      >{t("settings.name")}<input
        bind:value={name}
        required
        maxlength="100"
      /></label
    >
    <label
      >{t("admin.initialPassword")}<input
        type="password"
        bind:value={password}
        minlength="8"
        autocomplete="new-password"
        required
      /></label
    >
    <label class="check"
      ><input type="checkbox" bind:checked={isAdmin} />{t("admin.admin")}</label
    >
    <div>
      <button class="primary" type="submit">{t("admin.createUser")}</button>
    </div>
  </form>
{:else if active === "templates"}
  <section class="panel" aria-labelledby="tpls-title">
    <h2 id="tpls-title">{t("admin.nav.templates")}</h2>
    <p class="muted hint">{t("admin.templatesIntro")}</p>
    <ul class="list">
      {#each templates as tpl (tpl.id)}
        <li>
          <div>
            <strong>{tpl.name}</strong>
            <span class="muted">{tpl.description}</span>
          </div>
          <a
            class="btn"
            href={`data:application/json;charset=utf-8,${encodeURIComponent(JSON.stringify(tpl.data, null, 2))}`}
            download="{tpl.name}.json">{t("admin.export")}</a
          >
          <button
            type="button"
            onclick={() =>
              void run(t("psettings.templateDeleted"), async () => {
                await deleteProjectTemplate(tpl.id);
                templates = await listProjectTemplates();
              })}>{t("psettings.remove")}</button
          >
        </li>
      {/each}
    </ul>
  </section>
  <form
    class="panel"
    onsubmit={(e) => {
      e.preventDefault();
      void run(t("psettings.projectTemplateSaved"), async () => {
        await saveProjectTemplate({
          name: importName,
          data: JSON.parse(importText) as unknown,
        });
        importName = "";
        importText = "";
        templates = await listProjectTemplates();
      });
    }}
  >
    <h2>{t("admin.importTemplate")}</h2>
    <label
      >{t("psettings.templateName")}<input
        bind:value={importName}
        required
        maxlength="60"
      /></label
    >
    <label
      >{t("admin.templateJson")}<textarea
        bind:value={importText}
        rows="6"
        required></textarea></label
    >
    <div>
      <button class="primary" type="submit">{t("admin.importTemplate")}</button>
    </div>
  </form>
{:else if active === "rights"}
  <section class="panel" aria-labelledby="rights-title">
    <h2 id="rights-title">{t("admin.nav.rights")}</h2>
    <p class="muted hint">{t("admin.rightsIntro")}</p>
    <table>
      <thead>
        <tr>
          <th scope="col">{t("admin.action")}</th>
          {#each ["viewer", "member", "admin"] as r (r)}
            <th scope="col"
              >{t(`psettings.roles.${r}` as "psettings.roles.admin")}</th
            >
          {/each}
        </tr>
      </thead>
      <tbody>
        {#each rights as row (row.action)}
          <tr>
            <th scope="row"
              >{t(
                `admin.actions.${row.action.replace(".", "_")}` as "admin.actions.view",
              )}</th
            >
            {#each [row.viewer, row.member, row.admin] as ok, i (i)}
              <td
                ><span aria-hidden="true">{ok ? "✓" : "–"}</span><span
                  class="sr-only"
                  >{ok ? t("admin.allowed") : t("admin.denied")}</span
                ></td
              >
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
    <p class="muted hint">{t("admin.rightsTokens")}</p>
  </section>
{:else if active === "audit"}
  <section class="panel" aria-labelledby="audit-title">
    <h2 id="audit-title">{t("admin.nav.audit")}</h2>
    <form
      class="inline"
      onsubmit={(e) => {
        e.preventDefault();
        void loadAudit();
      }}
    >
      <input
        aria-label={t("admin.filterActor")}
        placeholder={t("admin.filterActor")}
        bind:value={fActor}
      />
      <input
        aria-label={t("admin.filterOperation")}
        placeholder={t("admin.filterOperation")}
        bind:value={fOperation}
      />
      <input
        aria-label={t("admin.filterTarget")}
        placeholder={t("admin.filterTarget")}
        bind:value={fTarget}
      />
      <button class="primary" type="submit">{t("admin.filter")}</button>
    </form>
    <div class="scroll">
      <table>
        <thead>
          <tr>
            <th scope="col">{t("admin.when")}</th>
            <th scope="col">{t("admin.actor")}</th>
            <th scope="col">{t("admin.operation")}</th>
            <th scope="col">{t("admin.target")}</th>
          </tr>
        </thead>
        <tbody>
          {#each audit as e (e.id)}
            <tr title={e.changes}>
              <td>{dateTime.format(e.ts)}</td>
              <td><code>{e.actor}</code></td>
              <td>{e.operation}</td>
              <td>{e.target}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    {#if audit.length >= 50}
      <div>
        <button type="button" onclick={() => void loadAudit(true)}
          >{t("admin.more")}</button
        >
      </div>
    {/if}
  </section>
{:else}
  <section class="panel" aria-labelledby="instance-title">
    <h2 id="instance-title">{t("admin.nav.instance")}</h2>
    <p class="muted hint">{t("admin.instanceIntro")}</p>
    <dl>
      {#each Object.entries(settings) as [k, v] (k)}
        <dt>{t(`admin.settings.${k}` as "admin.settings.upload_max_mb")}</dt>
        <dd>{String(v)}</dd>
      {/each}
    </dl>
  </section>
{/if}

<style>
  .subnav {
    display: flex;
    gap: var(--space-1);
    overflow-x: auto;
    border-bottom: 1px solid var(--color-border);
  }
  .subnav a {
    padding: 10px 16px;
    border-bottom: 2px solid transparent;
    color: var(--color-muted);
    font-weight: var(--atlas-fw-semibold);
    text-decoration: none;
    white-space: nowrap;
  }
  .subnav a[aria-current="page"] {
    color: var(--color-text);
    border-bottom-color: var(--atlas-primary);
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
  .btn {
    display: inline-flex;
    align-items: center;
    min-height: 38px;
    padding: 0 14px;
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    color: var(--color-text);
    font-weight: var(--atlas-fw-semibold);
    text-decoration: none;
  }
  .hint {
    margin: 0;
    font-size: var(--atlas-fs-sm);
  }
  label {
    display: grid;
    gap: 6px;
    max-width: 420px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
  }
  label.check {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .list li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: 10px 0;
    border-top: 1px solid var(--color-border);
  }
  .list li:first-child {
    border-top: none;
  }
  .list li.off {
    opacity: 0.65;
  }
  .list li > div {
    flex: 1;
    min-width: 12rem;
    display: grid;
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
    font-size: var(--atlas-fs-sm);
  }
  th,
  td {
    padding: 8px 10px;
    text-align: left;
    border-bottom: 1px solid var(--color-border);
  }
  td:not(:first-child) {
    text-align: left;
  }
  dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 8px var(--space-4);
    margin: 0;
  }
  dt {
    color: var(--color-muted);
  }
  dd {
    margin: 0;
    font-family: var(--font-mono);
  }
</style>
