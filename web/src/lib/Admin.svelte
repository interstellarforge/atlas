<script lang="ts">
  import AdminSystem from "./AdminSystem.svelte";
  import {
    type AdminApplication,
    type AdminOrganization,
    type AdminUser,
    ApiError,
    archiveAdminOrganization,
    type AuditEntry,
    clearMailSettings,
    createAdminOrganization,
    createInvite,
    createUser,
    decideApplication,
    decideDomain,
    deleteInstanceItemTemplate,
    deleteProjectTemplate,
    type DomainClaim,
    getAudit,
    getInstance,
    getMailSettings,
    getPolicy,
    getUpdateStatus,
    type Invite,
    type ItemTemplate,
    listAdminApplications,
    listAdminOrganizations,
    listAdminUsers,
    listDomainClaims,
    listInstanceItemTemplates,
    listInvites,
    listProjectTemplates,
    type MailSettings,
    patchAdminUser,
    type Policy,
    type ProjectTemplate,
    revokeInvite,
    saveInstanceItemTemplate,
    saveMailSettings,
    saveProjectTemplate,
    sendTestMail,
    setOrgMemberRole,
    setPolicy,
    type UpdateStatus,
    type User,
  } from "./api";
  import { t } from "./i18n";
  import PageHero from "./PageHero.svelte";
  import { formatSubitems, parseSubitems } from "./subitems";

  let { user, section }: { user: User; section: string } = $props();

  const sections = [
    "users",
    "requests",
    "organizations",
    "access",
    "templates",
    "audit",
    "logs",
    "mcp",
    "mail",
    "instance",
  ] as const;
  const active = $derived(
    (sections as readonly string[]).includes(section) ? section : "users",
  );
  const dateTime = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });

  let mail = $state<MailSettings | undefined>();
  let mailPassword = $state("");
  let mailClearPassword = $state(false);
  let policy = $state<Policy | undefined>();
  let invites = $state<Invite[]>([]);
  let inviteEmail = $state("");
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
  let userStatus = $state<"" | "active" | "suspended">("");
  const shownUsers = $derived(
    userStatus ? users.filter((u) => u.status === userStatus) : users,
  );
  let orgs = $state<AdminOrganization[]>([]);
  let appoint = $state<Record<string, string>>({});
  let claims = $state<DomainClaim[]>([]);
  let applications = $state<AdminApplication[]>([]);
  let appKeys = $state<Record<number, string>>({});
  let orgKey = $state("");
  let orgName = $state("");
  let orgAdminEmail = $state("");
  let settings = $state<Record<string, string | number | boolean>>({});
  let update = $state<UpdateStatus | undefined>();
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

  async function loadOrganizations(): Promise<void> {
    [orgs, applications, claims] = await Promise.all([
      listAdminOrganizations(),
      listAdminApplications(),
      listDomainClaims(),
    ]);
    for (const a of applications) appKeys[a.id] ??= a.key;
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

  async function saveMail(): Promise<void> {
    if (!mail) return;
    const { host, port, security, username, from_addr, from_name } = mail;
    mail = await saveMailSettings({
      host,
      port,
      security,
      username,
      from_addr,
      from_name,
      ...(mailClearPassword
        ? { password: "" }
        : mailPassword !== ""
          ? { password: mailPassword }
          : {}),
    });
    mailPassword = "";
    mailClearPassword = false;
  }

  $effect(() => {
    if (active === "mail")
      void getMailSettings()
        .then((m) => (mail = m))
        .catch(() => (error = t("admin.loadFailed")));
    if (active === "users" || active === "organizations")
      void loadUsers().catch(() => (error = t("admin.loadFailed")));
    if (active === "organizations" || active === "requests")
      void loadOrganizations().catch(() => (error = t("admin.loadFailed")));
    if (active === "access") {
      void getPolicy().then((p) => (policy = p));
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
    if (active === "instance") {
      void getInstance()
        .then((i) => (settings = i.settings))
        .catch(() => (error = t("admin.loadFailed")));
      void getUpdateStatus().then(
        (u) => (update = u),
        () => undefined, // advisory only
      );
    }
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
    <select aria-label={t("admin.statusFilter")} bind:value={userStatus}>
      <option value="">{t("admin.statusAll")}</option>
      <option value="active">{t("admin.statusActive")}</option>
      <option value="suspended">{t("admin.deactivated")}</option>
    </select>
    <ul class="list">
      {#each shownUsers as u (u.id)}
        <li class:off={u.status !== "active"}>
          <div>
            <strong>{u.name}</strong>
            <span class="muted">{u.email}</span>
            <span class="muted"
              >{new Date(u.created_at).toLocaleDateString()}{u.organizations
                ?.length
                ? ` · ${u.organizations.map((o) => `${o.key} (${o.role})`).join(", ")}`
                : ""}</span
            >
          </div>
          {#if u.status === "suspended"}<span class="chip warn"
              >{t("admin.deactivated")}</span
            >{/if}
          {#if u.is_superadmin}<span class="chip type">{t("admin.admin")}</span
            >{/if}
          {#if u.must_change_password}<span class="chip warn"
              >{t("admin.mustChange")}</span
            >{/if}
          <button
            type="button"
            disabled={!u.is_superadmin && u.status !== "active"}
            onclick={() =>
              void run(t("settings.saved"), async () => {
                await patchAdminUser(u.id, {
                  is_superadmin: !u.is_superadmin,
                });
                await loadUsers();
              })}
            >{u.is_superadmin
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
            disabled={u.status !== "active"}
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
                  active: u.status === "suspended",
                });
                await loadUsers();
              })}
            >{u.status === "suspended"
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
        await createUser({ email, name, password, is_superadmin: isAdmin });
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
{:else if active === "requests"}
  <section class="panel" aria-labelledby="applications-title">
    <h2 id="applications-title">{t("admin.appsTitle")}</h2>
    {#if applications.length === 0}
      <p class="muted">{t("admin.appsEmpty")}</p>
    {:else}
      <ul class="list">
        {#each applications as a (a.id)}
          <li>
            <div>
              <strong>{a.name}</strong>
              <span class="muted"
                >{t("admin.appBy", {
                  name: a.applicant_name,
                  email: a.applicant_email,
                })}</span
              >
              {#if a.note}<span class="muted">{a.note}</span>{/if}
            </div>
            <label>
              {t("admin.appKey")}
              <input bind:value={appKeys[a.id]} maxlength="10" />
            </label>
            <button
              type="button"
              onclick={() =>
                void run(t("admin.appApproved"), async () => {
                  await decideApplication(a.id, "approve", {
                    key: appKeys[a.id] ?? a.key,
                  });
                  await loadOrganizations();
                })}>{t("admin.approve")}</button
            >
            <button
              type="button"
              onclick={() =>
                void run(t("admin.appRejected"), async () => {
                  await decideApplication(a.id, "reject");
                  await loadOrganizations();
                })}>{t("admin.reject")}</button
            >
          </li>
        {/each}
      </ul>
    {/if}
  </section>
  <section class="panel" aria-labelledby="claims-title">
    <h2 id="claims-title">{t("admin.domainsTitle")}</h2>
    <p class="muted hint">{t("admin.domainsIntro")}</p>
    {#if claims.length === 0}
      <p class="muted">{t("admin.domainsEmpty")}</p>
    {:else}
      <ul class="list">
        {#each claims as c (c.id)}
          <li>
            <div>
              <strong>{c.domain}</strong>
              <span class="muted"
                >{t("admin.domainBy", {
                  name: c.organization_name,
                  key: c.organization_key,
                })}</span
              >
            </div>
            <button
              type="button"
              onclick={() =>
                void run(t("admin.domainConfirmed"), async () => {
                  await decideDomain(c.id, "approve");
                  await loadOrganizations();
                })}>{t("admin.approve")}</button
            >
            <button
              type="button"
              onclick={() =>
                void run(t("admin.domainRejected"), async () => {
                  await decideDomain(c.id, "reject");
                  await loadOrganizations();
                })}>{t("admin.reject")}</button
            >
          </li>
        {/each}
      </ul>
    {/if}
  </section>
{:else if active === "organizations"}
  <section class="panel" aria-labelledby="orgs-title">
    <h2 id="orgs-title">{t("admin.orgsTitle")}</h2>
    <p class="muted hint">{t("admin.orgsIntro")}</p>
    <ul class="list">
      {#each orgs as o (o.key)}
        <li class:off={o.status !== "active"}>
          <div>
            <strong>{o.name}</strong>
            <span class="muted">{o.key}</span>
          </div>
          {#if o.status !== "active"}<span class="chip warn"
              >{t("admin.orgArchived")}</span
            >{/if}
          {#if o.no_active_admin}<span class="chip warn"
              >{t("admin.orgNoAdmin")}</span
            >{/if}
          <span class="muted"
            >{t("admin.orgAdmins")}: {o.admins.join(", ") || "-"}</span
          >
          <span class="muted"
            >{t("admin.orgMembers", { count: o.members })}</span
          >
          <span class="muted"
            >{t("admin.orgProjects", { count: o.projects })}</span
          >
          <button
            type="button"
            onclick={() =>
              void run(t("settings.saved"), async () => {
                await archiveAdminOrganization(
                  o.key,
                  o.status === "active" ? "archive" : "restore",
                );
                await loadOrganizations();
              })}
            >{o.status === "active"
              ? t("admin.orgArchive")
              : t("admin.orgRestore")}</button
          >
          {#if o.no_active_admin && o.status === "active"}
            <form
              class="inline"
              onsubmit={(e) => {
                e.preventDefault();
                void run(t("admin.orgAppointed"), async () => {
                  await setOrgMemberRole(
                    o.key,
                    Number(appoint[o.key]),
                    "admin",
                  );
                  await loadOrganizations();
                });
              }}
            >
              <select
                aria-label={t("admin.orgAppoint")}
                bind:value={appoint[o.key]}
                required
              >
                <option value="">{t("admin.orgAppoint")}</option>
                {#each users.filter((u) => u.status === "active") as u (u.id)}
                  <option value={String(u.id)}>{u.name}</option>
                {/each}
              </select>
              <button class="primary" type="submit"
                >{t("admin.orgAppointSubmit")}</button
              >
            </form>
          {/if}
        </li>
      {/each}
    </ul>
  </section>
  <form
    class="panel"
    onsubmit={(e) => {
      e.preventDefault();
      void run(t("admin.orgCreated"), async () => {
        await createAdminOrganization({
          key: orgKey,
          name: orgName,
          admin_email: orgAdminEmail,
        });
        orgKey = "";
        orgName = "";
        orgAdminEmail = "";
        await loadOrganizations();
      });
    }}
  >
    <h2>{t("admin.orgCreate")}</h2>
    <label>
      {t("admin.orgKey")}
      <input bind:value={orgKey} required maxlength="10" />
    </label>
    <label>
      {t("admin.orgName")}
      <input bind:value={orgName} required />
    </label>
    <label>
      {t("admin.orgAdminEmail")}
      <input type="email" bind:value={orgAdminEmail} required />
    </label>
    <div>
      <button class="primary" type="submit">{t("admin.orgCreate")}</button>
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
      <label>
        {t("admin.orgCreation")}
        <select bind:value={policy.organization_creation}>
          <option value="admin_only">{t("admin.orgCreationAdminOnly")}</option>
          <option value="apply">{t("admin.orgCreationApply")}</option>
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
        {t("admin.doneVisibleDays")}
        <input
          type="number"
          min="0"
          max="3650"
          bind:value={policy.done_visible_days}
          required
        />
        <span class="hint">{t("admin.doneVisibleDaysHint")}</span>
      </label>
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
            days: inviteDays,
          });
          inviteLink = `${location.origin}/#/register?invite=${made.code}`;
          inviteEmail = "";
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
{:else if active === "logs" || active === "mcp"}
  <AdminSystem kind={active} />
{:else if active === "mail"}
  {#if mail}
    <form
      class="panel"
      aria-labelledby="mail-title"
      onsubmit={(e) => {
        e.preventDefault();
        void run(t("settings.saved"), saveMail);
      }}
    >
      <h2 id="mail-title">{t("admin.mail")}</h2>
      <p class="muted hint">{t("admin.mailIntro")}</p>
      <p class="muted hint">
        {t(`admin.mailSource.${mail.source}` as "admin.mailSource.none")}
      </p>
      <label>
        {t("admin.mailHost")}
        <input bind:value={mail.host} required placeholder="smtp.example.com" />
      </label>
      <label>
        {t("admin.mailPort")}
        <input
          type="number"
          min="1"
          max="65535"
          bind:value={mail.port}
          required
        />
      </label>
      <label>
        {t("admin.mailSecurity")}
        <select bind:value={mail.security}>
          {#each ["starttls", "tls", "none"] as o (o)}
            <option value={o}
              >{t(
                `admin.mailSecurityOptions.${o}` as "admin.mailSecurityOptions.tls",
              )}</option
            >
          {/each}
        </select>
      </label>
      <label>
        {t("admin.mailUsername")}
        <input bind:value={mail.username} autocomplete="off" />
      </label>
      <label>
        {t("admin.mailPassword")}
        <input
          type="password"
          bind:value={mailPassword}
          autocomplete="new-password"
          disabled={mailClearPassword}
        />
      </label>
      {#if mail.password_set}
        <p class="muted hint">{t("admin.mailPasswordSet")}</p>
        <label class="check">
          <input type="checkbox" bind:checked={mailClearPassword} />
          {t("admin.mailPasswordClear")}
        </label>
      {/if}
      <label>
        {t("admin.mailFromAddr")}
        <input type="email" bind:value={mail.from_addr} required />
      </label>
      <label>
        {t("admin.mailFromName")}
        <input bind:value={mail.from_name} />
      </label>
      <div class="row">
        <button class="primary" type="submit">{t("admin.mailSave")}</button>
        <button
          type="button"
          disabled={!mail.configured}
          onclick={() => void run(t("admin.mailTestSent"), sendTestMail)}
          >{t("admin.mailTest")}</button
        >
        {#if mail.source === "database"}
          <button
            type="button"
            onclick={() =>
              void run(t("admin.mailCleared"), async () => {
                mail = await clearMailSettings();
              })}>{t("admin.mailUseEnv")}</button
          >
        {/if}
      </div>
    </form>
  {/if}
{:else}
  <section class="panel" aria-labelledby="instance-title">
    <h2 id="instance-title">{t("admin.nav.instance")}</h2>
    <p class="muted hint">{t("admin.instanceIntro")}</p>
    <dl>
      <dt>{t("admin.version.label")}</dt>
      <dd>
        {update?.current ?? ""}
        {#if update?.available && update.url}
          <a href={update.url} target="_blank" rel="noopener noreferrer"
            >{t("admin.version.available", { latest: update.latest ?? "" })}</a
          >
        {:else if update?.latest && update.current === update.latest}
          <span class="muted">{t("admin.version.upToDate")}</span>
        {:else if update?.latest}
          <span class="muted"
            >{t("admin.version.devBuild", { latest: update.latest })}</span
          >
        {:else if update}
          <span class="muted">{t("admin.version.unknown")}</span>
        {/if}
      </dd>
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
