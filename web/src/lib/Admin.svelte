<script lang="ts">
  import {
    type AdminUser,
    ApiError,
    type AuditEntry,
    createInvite,
    createUser,
    deleteInstanceItemTemplate,
    deleteProjectTemplate,
    deleteRole,
    getAudit,
    getInstance,
    getPolicy,
    type Invite,
    type ItemTemplate,
    listAdminUsers,
    listInstanceItemTemplates,
    listInvites,
    listProjectTemplates,
    listRoles,
    patchAdminUser,
    type Policy,
    type ProjectTemplate,
    revokeInvite,
    type Role,
    saveInstanceItemTemplate,
    saveProjectTemplate,
    saveRole,
    setPolicy,
    type User,
  } from "./api";
  import { t } from "./i18n";
  import PageHero from "./PageHero.svelte";
  import { formatSubitems, parseSubitems } from "./subitems";

  let { user, section }: { user: User; section: string } = $props();

  const sections = [
    "users",
    "access",
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

  let policy = $state<Policy | undefined>();
  let invites = $state<Invite[]>([]);
  let inviteEmail = $state("");
  let inviteAdmin = $state(false);
  let inviteDays = $state(7);
  let inviteLink = $state("");
  let itemTemplates = $state<ItemTemplate[]>([]);
  let it = $state({
    name: "",
    type: "task",
    prefix: "",
    description: "",
    subitems: "",
  });
  let templates = $state<ProjectTemplate[]>([]);
  let importText = $state("");
  let importName = $state("");
  let users = $state<AdminUser[]>([]);
  let roles = $state<Role[]>([]);
  let roleActions = $state<string[]>([]);
  let newRole = $state("");
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

  function roleLabel(name: string): string {
    return ["admin", "member", "viewer"].includes(name)
      ? t(`psettings.roles.${name}` as "psettings.roles.admin")
      : name;
  }

  async function loadRoles(): Promise<void> {
    try {
      const r = await listRoles();
      roles = r.items;
      roleActions = r.actions;
    } catch {
      error = t("admin.loadFailed");
    }
  }

  function toggleAction(r: Role, action: string, on: boolean): Promise<void> {
    const actions = on
      ? [...r.actions, action]
      : r.actions.filter((x) => x !== action);
    return run(t("admin.roleSaved"), async () => {
      await saveRole(r.name, actions);
      await loadRoles();
    });
  }

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

  function exportHref(format: string): string {
    const parts = [`format=${format}`];
    for (const [k, v] of [
      ["actor", fActor],
      ["operation", fOperation],
      ["target", fTarget],
    ] as const)
      if (v.trim()) parts.push(`${k}=${encodeURIComponent(v.trim())}`);
    return `/api/v1/admin/audit/export?${parts.join("&")}`;
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
    if (active === "access") {
      void getPolicy().then((p) => (policy = p));
      void loadRoles();
      void listInvites().then((l) => (invites = l));
    }
    if (active === "templates")
      void Promise.all([listProjectTemplates(), listInstanceItemTemplates()])
        .then(([list, items]) => {
          templates = list;
          itemTemplates = items;
        })
        .catch(() => (error = t("admin.loadFailed")));
    if (active === "audit")
      void loadAudit().catch(() => (error = t("admin.loadFailed")));
    if (active === "rights") void loadRoles();
    if (active === "instance")
      void getInstance()
        .then((i) => (settings = i.settings))
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
          {#if u.must_change_password}<span class="chip warn"
              >{t("admin.mustChange")}</span
            >{/if}
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
            onclick={() =>
              void run(t("admin.totpReset"), async () => {
                await patchAdminUser(u.id, { reset_totp: true });
              })}>{t("admin.resetTotp")}</button
          >
          <button
            type="button"
            disabled={Boolean(u.deactivated_at)}
            onclick={() =>
              void run(t("admin.passwordForced"), async () => {
                await patchAdminUser(u.id, { force_password_reset: true });
                await loadUsers();
              })}>{t("admin.forcePassword")}</button
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
{:else if active === "access"}
  {#if policy}
    <form
      class="panel"
      onsubmit={(e) => {
        e.preventDefault();
        if (!policy) return;
        const next = policy;
        void run(t("settings.saved"), async () => {
          policy = await setPolicy(next);
        });
      }}
    >
      <h2>{t("admin.policy")}</h2>
      <p class="muted hint">{t("admin.policyIntro")}</p>
      <label>
        {t("admin.registration")}
        <select bind:value={policy.registration}>
          <option value="closed">{t("admin.regClosed")}</option>
          <option value="invite">{t("admin.regInvite")}</option>
          <option value="open">{t("admin.regOpen")}</option>
        </select>
      </label>
      {#if policy.registration === "open"}
        <label>
          {t("admin.allowedDomain")}
          <input bind:value={policy.allowed_domain} placeholder="example.com" />
        </label>
      {/if}
      <label>
        {t("admin.minPassword")}
        <input
          type="number"
          min="8"
          max="128"
          bind:value={policy.min_password_length}
          required
        />
      </label>
      <label>
        {t("admin.sessionDays")}
        <input
          type="number"
          min="1"
          max="90"
          bind:value={policy.session_days}
          required
        />
      </label>
      <label>
        {t("admin.defaultRole")}
        <select bind:value={policy.default_role}>
          {#each roles as r (r.name)}
            <option value={r.name}>{roleLabel(r.name)}</option>
          {/each}
        </select>
      </label>
      <p class="muted hint">{t("admin.defaultRoleHint")}</p>
      <div>
        <button class="primary" type="submit">{t("settings.save")}</button>
      </div>
    </form>
  {/if}
  <section class="panel" aria-labelledby="inv-title">
    <h2 id="inv-title">{t("admin.invites")}</h2>
    <p class="muted hint">{t("admin.invitesIntro")}</p>
    {#if inviteLink}
      <div class="banner ok" role="status">
        <p>{t("admin.inviteOnce")}</p>
        <code class="secret">{inviteLink}</code>
      </div>
    {/if}
    <ul class="list">
      {#each invites as inv (inv.id)}
        <li class:off={Boolean(inv.used_at)}>
          <div>
            <strong>{inv.email || t("admin.anyEmail")}</strong>
            <span class="muted"
              >{inv.used_at
                ? t("admin.inviteUsed", { date: dateTime.format(inv.used_at) })
                : t("admin.inviteExpires", {
                    date: dateTime.format(inv.expires_at),
                  })}</span
            >
          </div>
          {#if inv.is_admin}<span class="chip type">{t("admin.admin")}</span
            >{/if}
          {#if !inv.used_at}
            <button
              type="button"
              onclick={() =>
                void run(t("admin.inviteRevoked"), async () => {
                  await revokeInvite(inv.id);
                  invites = await listInvites();
                })}>{t("psettings.remove")}</button
            >
          {/if}
        </li>
      {/each}
    </ul>
    <form
      class="inline"
      onsubmit={(e) => {
        e.preventDefault();
        void run(t("admin.inviteCreated"), async () => {
          const made = await createInvite({
            email: inviteEmail,
            is_admin: inviteAdmin,
            days: inviteDays,
          });
          inviteLink = `${location.origin}/#/register?invite=${made.code}`;
          inviteEmail = "";
          inviteAdmin = false;
          invites = await listInvites();
        });
      }}
    >
      <input
        type="email"
        aria-label={t("admin.inviteEmail")}
        placeholder={t("admin.inviteEmail")}
        bind:value={inviteEmail}
      />
      <input
        type="number"
        min="1"
        max="30"
        aria-label={t("admin.inviteDays")}
        bind:value={inviteDays}
      />
      <label class="check"
        ><input type="checkbox" bind:checked={inviteAdmin} />{t(
          "admin.admin",
        )}</label
      >
      <button class="primary" type="submit">{t("admin.createInvite")}</button>
    </form>
  </section>
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
  <section class="panel" aria-labelledby="itpl-title">
    <h2 id="itpl-title">{t("admin.instanceItemTemplates")}</h2>
    <p class="muted hint">{t("admin.instanceItemTemplatesIntro")}</p>
    <ul class="list">
      {#each itemTemplates as tpl (tpl.id)}
        <li>
          <div>
            <strong>{tpl.name}</strong>
            <span class="muted"
              >{t(`types.${tpl.type}` as "types.epic")}{tpl.subitems.length > 0
                ? ` · ${t("psettings.subitemCount", { count: formatSubitems(tpl.subitems).split("\n").length })}`
                : ""}</span
            >
          </div>
          <button
            type="button"
            onclick={() =>
              void run(t("psettings.templateDeleted"), async () => {
                await deleteInstanceItemTemplate(tpl.id);
                itemTemplates = await listInstanceItemTemplates();
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
          await saveInstanceItemTemplate({
            name: it.name,
            type: it.type,
            title_prefix: it.prefix,
            description: it.description,
            priority: "normal",
            labels: [],
            status: "",
            checklist: [],
            subitems: parseSubitems(it.subitems),
          });
          it = {
            name: "",
            type: "task",
            prefix: "",
            description: "",
            subitems: "",
          };
          itemTemplates = await listInstanceItemTemplates();
        });
      }}
    >
      <label
        >{t("psettings.templateName")}<input
          bind:value={it.name}
          required
          maxlength="60"
        /></label
      >
      <label>
        {t("board.type")}
        <select bind:value={it.type}>
          {#each ["epic", "story", "task", "bug"] as typ (typ)}
            <option value={typ}>{t(`types.${typ}` as "types.epic")}</option>
          {/each}
        </select>
      </label>
      <label
        >{t("psettings.titlePrefix")}<input
          bind:value={it.prefix}
          maxlength="100"
        /></label
      >
      <label
        >{t("item.description")}<textarea bind:value={it.description} rows="3"
        ></textarea></label
      >
      <label
        >{t("psettings.subitems")}<textarea bind:value={it.subitems} rows="4"
        ></textarea></label
      >
      <p class="muted hint">{t("psettings.subitemsHint")}</p>
      <div>
        <button class="primary" type="submit"
          >{t("psettings.saveTemplate")}</button
        >
      </div>
    </form>
  </section>
{:else if active === "rights"}
  <section class="panel" aria-labelledby="rights-title">
    <h2 id="rights-title">{t("admin.nav.rights")}</h2>
    <p class="muted hint">{t("admin.rightsIntro")}</p>
    <table>
      <thead>
        <tr>
          <th scope="col">{t("admin.action")}</th>
          {#each roles as r (r.name)}
            <th scope="col"
              >{roleLabel(r.name)}
              {#if !r.builtin}
                <button
                  type="button"
                  onclick={() =>
                    void run(t("admin.roleDeleted"), async () => {
                      await deleteRole(r.name);
                      await loadRoles();
                    })}>{t("admin.deleteRole")}</button
                >
              {/if}</th
            >
          {/each}
        </tr>
      </thead>
      <tbody>
        {#each roleActions as action (action)}
          <tr>
            <th scope="row"
              >{t(
                `admin.actions.${action.replace(".", "_")}` as "admin.actions.view",
              )}</th
            >
            {#each roles as r (r.name)}
              <td
                ><input
                  type="checkbox"
                  checked={r.actions.includes(action)}
                  disabled={r.name === "admin" || action === "view"}
                  aria-label="{roleLabel(r.name)}: {t(
                    `admin.actions.${action.replace('.', '_')}` as 'admin.actions.view',
                  )}"
                  onchange={(e) =>
                    void toggleAction(r, action, e.currentTarget.checked)}
                /></td
              >
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
    <form
      class="inline"
      onsubmit={(e) => {
        e.preventDefault();
        void run(t("admin.roleSaved"), async () => {
          await saveRole(newRole.trim(), ["view"]);
          newRole = "";
          await loadRoles();
        });
      }}
    >
      <input
        aria-label={t("admin.roleName")}
        placeholder={t("admin.roleName")}
        bind:value={newRole}
        required
        maxlength="32"
      />
      <button class="primary" type="submit">{t("admin.addRole")}</button>
    </form>
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
      <a class="btn" href={exportHref("csv")} download="atlas-audit.csv"
        >{t("admin.exportCsv")}</a
      >
      <a class="btn" href={exportHref("json")} download="atlas-audit.jsonl"
        >{t("admin.exportJson")}</a
      >
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
  .stack {
    display: grid;
    gap: var(--space-3);
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
