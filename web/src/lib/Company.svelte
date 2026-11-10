<script lang="ts">
  import {
    ApiError,
    type AuditEntry,
    claimDomain,
    createOrgInvite,
    decideJoinRequest,
    getOrgSettings,
    grantSupport,
    type JoinRequest,
    listJoinRequests,
    listOrgAudit,
    listOrgDomains,
    listOrgInvites,
    listOrgMembers,
    listProjectTemplates,
    listSupportGrants,
    type Organization,
    type OrgDomain,
    type OrgInvite,
    type OrgMember,
    type OrgSettings,
    type ProjectTemplate,
    removeOrgDomain,
    removeOrgLogo,
    removeOrgMember,
    revokeOrgInvite,
    revokeSupport,
    setDomainMode,
    setJoinLink,
    setOrgMemberRole,
    type SupportGrant,
    updateOrgSettings,
    uploadOrgLogo,
    type User,
  } from "./api";
  import { t } from "./i18n";
  import PageHero from "./PageHero.svelte";

  let {
    user,
    orgs,
    orgKey,
    section,
    onchange,
  }: {
    user: User;
    orgs: Organization[];
    orgKey: string;
    section: string;
    /** The organizations changed (a logo was set or removed): reload them. */
    onchange: () => void;
  } = $props();

  const sections = [
    "members",
    "invites",
    "requests",
    "domains",
    "settings",
    "support",
    "audit",
  ] as const;
  type Section = (typeof sections)[number];
  const active = $derived<Section>(
    (sections as readonly string[]).includes(section)
      ? (section as Section)
      : "members",
  );
  const current = $derived(orgs.find((o) => o.key === orgKey) ?? orgs[0]);
  const key = $derived(current?.key ?? "");
  const dateTime = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });

  let notice = $state("");
  let error = $state("");
  let members = $state<OrgMember[]>([]);
  let invites = $state<OrgInvite[]>([]);
  let requests = $state<JoinRequest[]>([]);
  let domains = $state<OrgDomain[]>([]);
  let settings = $state<OrgSettings | undefined>();
  let templates = $state<ProjectTemplate[]>([]);
  let logoVersion = $state(0);
  let grants = $state<SupportGrant[]>([]);
  let audit = $state<AuditEntry[]>([]);
  let moreAudit = $state(true);

  // forms
  let inviteEmail = $state("");
  let inviteProjects = $state("");
  let inviteDays = $state(7);
  let inviteLink = $state("");
  let domainName = $state("");
  let domainMode = $state<"request" | "auto">("request");
  let supportLevel = $state<"read_only" | "full">("read_only");
  let supportHours = $state(24);
  let supportForever = $state(false);
  let form = $state<{
    password: string;
    session: string;
    tokenDays: string;
    doneDays: string;
    template: string;
    creation: "admins" | "members";
    twoFactor: boolean;
    tokens: boolean;
  }>({
    password: "",
    session: "",
    tokenDays: "",
    doneDays: "",
    template: "",
    creation: "admins",
    twoFactor: false,
    tokens: true,
  });

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
      error = t("company.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }

  async function load(): Promise<void> {
    if (!key) return;
    try {
      if (active === "members") members = await listOrgMembers(key);
      if (active === "invites") invites = await listOrgInvites(key);
      if (active === "requests") {
        requests = await listJoinRequests(key, "pending");
        settings = await getOrgSettings(key);
      }
      if (active === "domains") domains = await listOrgDomains(key);
      if (active === "support") grants = await listSupportGrants(key);
      if (active === "audit") {
        audit = await listOrgAudit(key);
        moreAudit = audit.length > 0;
      }
      if (active === "settings") {
        [settings, templates] = await Promise.all([
          getOrgSettings(key),
          listProjectTemplates(),
        ]);
        form = {
          password: settings.password_min_length?.toString() ?? "",
          session: settings.session_days?.toString() ?? "",
          tokenDays: settings.token_max_days?.toString() ?? "",
          doneDays: settings.done_visible_days?.toString() ?? "",
          template: settings.default_project_template?.toString() ?? "",
          creation: settings.project_creation,
          twoFactor: settings.require_2fa,
          tokens: settings.tokens_allowed,
        };
      }
    } catch (e) {
      error = t("company.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }

  $effect(() => {
    notice = error = inviteLink = "";
    void load();
  });

  // A number input binds a number (or null when empty), the form keeps strings: accept both.
  const num = (v: unknown): number | null =>
    v === null || v === undefined || v === "" ? null : Number(v);

  async function saveSettings(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    await run(t("company.saved"), async () => {
      settings = await updateOrgSettings(key, {
        password_min_length: num(form.password),
        session_days: num(form.session),
        token_max_days: num(form.tokenDays),
        done_visible_days: num(form.doneDays),
        default_project_template: num(form.template),
        project_creation: form.creation,
        require_2fa: form.twoFactor,
        tokens_allowed: form.tokens,
      });
    });
  }

  async function invite(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    await run(t("company.invites.sent"), async () => {
      const projects = inviteProjects
        .split(",")
        .map((p) => p.trim())
        .filter(Boolean);
      const res = await createOrgInvite(key, {
        email: inviteEmail,
        projects,
        days: inviteDays,
      });
      inviteLink = `${location.origin}${location.pathname}#/invite/${res.token}`;
      inviteEmail = inviteProjects = "";
      invites = await listOrgInvites(key);
    });
  }

  const joinLink = $derived(
    `${location.origin}${location.pathname}#/join/${key}`,
  );

  function when(ts: number | null): string {
    return ts
      ? dateTime.format(new Date(ts))
      : t("company.support.untilRevoked");
  }
</script>

<PageHero title={t("company.title")} description={t("company.intro")} />

{#if orgs.length > 1}
  <label class="switch">
    {t("company.switch")}
    <select
      value={key}
      onchange={(e) => {
        location.hash = `#/company/${e.currentTarget.value}/${active}`;
      }}
    >
      {#each orgs as o (o.key)}
        <option value={o.key}>{o.name} ({o.key})</option>
      {/each}
    </select>
  </label>
{/if}

<nav class="subnav" aria-label={t("company.sections")}>
  {#each sections as s (s)}
    <a
      href="#/company/{key}/{s}"
      aria-current={active === s ? "page" : undefined}
      >{t(`company.nav.${s}` as "company.nav.members")}</a
    >
  {/each}
</nav>

{#if notice}<p class="banner ok" role="status">{notice}</p>{/if}
{#if error}<p class="banner error" role="alert">{error}</p>{/if}

{#if active === "members"}
  <section class="panel">
    <h2>{t("company.nav.members")}</h2>
    <ul class="list">
      {#each members as m (m.user_id)}
        <li class:off={!m.active}>
          <div>
            <strong>{m.name}</strong>
            <span class="muted">{m.email}</span>
          </div>
          <select
            aria-label={t("company.members.role")}
            value={m.role}
            disabled={m.user_id === user.id}
            onchange={(e) =>
              void run(t("company.saved"), async () => {
                await setOrgMemberRole(
                  key,
                  m.user_id,
                  e.currentTarget.value as "admin" | "user",
                );
                members = await listOrgMembers(key);
              })}
          >
            <option value="admin">{t("company.members.admin")}</option>
            <option value="user">{t("company.members.user")}</option>
          </select>
          <button
            type="button"
            disabled={m.user_id === user.id}
            onclick={() => {
              if (confirm(t("company.members.confirmRemove", { name: m.name })))
                void run(t("company.members.removed"), async () => {
                  await removeOrgMember(key, m.user_id);
                  members = await listOrgMembers(key);
                });
            }}>{t("company.members.remove")}</button
          >
        </li>
      {/each}
    </ul>
    <p class="muted hint">{t("company.members.hint")}</p>
  </section>
{:else if active === "invites"}
  <form class="panel" onsubmit={invite}>
    <h2>{t("company.invites.title")}</h2>
    <label>
      {t("company.invites.email")}
      <input type="email" bind:value={inviteEmail} required />
    </label>
    <label>
      {t("company.invites.projects")}
      <input bind:value={inviteProjects} placeholder="ABC, DEF" />
    </label>
    <label>
      {t("company.invites.days")}
      <input type="number" min="1" max="30" bind:value={inviteDays} />
    </label>
    <div>
      <button class="primary" type="submit">{t("company.invites.send")}</button>
    </div>
    {#if inviteLink}
      <div class="banner ok">
        <p>{t("company.invites.link")}</p>
        <code class="secret">{inviteLink}</code>
      </div>
    {/if}
  </form>
  <section class="panel">
    <ul class="list">
      {#each invites as i (i.id)}
        <li class:off={i.state !== "pending"}>
          <div>
            <strong>{i.email}</strong>
            <span class="muted"
              >{t(
                `company.invites.state.${i.state}` as "company.invites.state.pending",
              )}
              · {when(i.expires_at)}{i.projects.length
                ? ` · ${i.projects.join(", ")}`
                : ""}</span
            >
          </div>
          {#if i.state === "pending"}
            <button
              type="button"
              onclick={() =>
                void run(t("company.invites.revoked"), async () => {
                  await revokeOrgInvite(key, i.id);
                  invites = await listOrgInvites(key);
                })}>{t("company.invites.revoke")}</button
            >
          {/if}
        </li>
      {:else}
        <li class="muted">{t("company.invites.empty")}</li>
      {/each}
    </ul>
  </section>
{:else if active === "requests"}
  <section class="panel">
    <h2>{t("company.requests.title")}</h2>
    <label class="check">
      <input
        type="checkbox"
        checked={settings?.join_link_enabled ?? false}
        onchange={(e) =>
          void run(t("company.saved"), async () => {
            await setJoinLink(key, e.currentTarget.checked);
            settings = await getOrgSettings(key);
          })}
      />
      {t("company.requests.linkOn")}
    </label>
    {#if settings?.join_link_enabled}
      <code class="secret">{joinLink}</code>
    {/if}
    <ul class="list">
      {#each requests as r (r.id)}
        <li>
          <div>
            <strong>{r.user_name}</strong>
            <span class="muted">{r.user_email} · {when(r.created_at)}</span>
          </div>
          {#each ["approve", "reject"] as d (d)}
            <button
              type="button"
              onclick={() =>
                void run(t("company.requests.decided"), async () => {
                  await decideJoinRequest(key, r.id, d as "approve" | "reject");
                  requests = await listJoinRequests(key, "pending");
                })}
              >{t(
                `company.requests.${d}` as "company.requests.approve",
              )}</button
            >
          {/each}
        </li>
      {:else}
        <li class="muted">{t("company.requests.empty")}</li>
      {/each}
    </ul>
  </section>
{:else if active === "domains"}
  <form
    class="panel"
    onsubmit={(e) => {
      e.preventDefault();
      void run(t("company.domains.claimed"), async () => {
        await claimDomain(key, { domain: domainName, mode: domainMode });
        domainName = "";
        domains = await listOrgDomains(key);
      });
    }}
  >
    <h2>{t("company.domains.title")}</h2>
    <p class="muted hint">{t("company.domains.intro")}</p>
    <label>
      {t("company.domains.domain")}
      <input bind:value={domainName} placeholder="example.com" required />
    </label>
    <label>
      {t("company.domains.mode")}
      <select bind:value={domainMode}>
        <option value="request">{t("company.domains.request")}</option>
        <option value="auto">{t("company.domains.auto")}</option>
      </select>
    </label>
    <div>
      <button class="primary" type="submit">{t("company.domains.claim")}</button
      >
    </div>
  </form>
  <section class="panel">
    <ul class="list">
      {#each domains as d (d.id)}
        <li>
          <div>
            <strong>{d.domain}</strong>
            <span class="muted"
              >{t(
                `company.domains.status.${d.status}` as "company.domains.status.pending",
              )}</span
            >
          </div>
          <select
            aria-label={t("company.domains.mode")}
            value={d.mode}
            onchange={(e) =>
              void run(t("company.saved"), async () => {
                await setDomainMode(
                  key,
                  d.id,
                  e.currentTarget.value as "request" | "auto",
                );
                domains = await listOrgDomains(key);
              })}
          >
            <option value="request">{t("company.domains.request")}</option>
            <option value="auto">{t("company.domains.auto")}</option>
          </select>
          <button
            type="button"
            onclick={() =>
              void run(t("company.domains.removed"), async () => {
                await removeOrgDomain(key, d.id);
                domains = await listOrgDomains(key);
              })}>{t("company.domains.remove")}</button
          >
        </li>
      {:else}
        <li class="muted">{t("company.domains.empty")}</li>
      {/each}
    </ul>
  </section>
{:else if active === "settings" && settings}
  <form class="panel" onsubmit={saveSettings}>
    <h2>{t("company.settings.title")}</h2>
    <label>
      {t("company.settings.password", {
        floor: settings.instance.min_password_length,
      })}
      <input
        type="number"
        min={settings.instance.min_password_length}
        max="128"
        bind:value={form.password}
        placeholder={String(settings.instance.min_password_length)}
      />
    </label>
    <label>
      {t("company.settings.session", {
        instance: settings.instance.session_days,
      })}
      <input type="number" min="1" max="90" bind:value={form.session} />
    </label>
    <label>
      {t("company.settings.creation")}
      <select bind:value={form.creation}>
        <option value="admins">{t("company.settings.creationAdmins")}</option>
        <option value="members">{t("company.settings.creationMembers")}</option>
      </select>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={form.twoFactor} />
      {t("company.settings.twoFactor")}
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={form.tokens} />
      {t("company.settings.tokens")}
    </label>
    <label>
      {t("company.settings.tokenDays")}
      <input type="number" min="1" max="365" bind:value={form.tokenDays} />
    </label>
    <label>
      {t("company.settings.doneDays")}
      <input type="number" min="0" max="3650" bind:value={form.doneDays} />
    </label>
    <label>
      {t("company.settings.template")}
      <select bind:value={form.template}>
        <option value="">{t("company.settings.templateNone")}</option>
        {#each templates as tpl (tpl.id)}
          <option value={String(tpl.id)}>{tpl.name}</option>
        {/each}
      </select>
    </label>
    <p class="muted hint">{t("company.settings.hint")}</p>
    <div>
      <button class="primary" type="submit">{t("company.save")}</button>
    </div>
  </form>
  <section class="panel">
    <h2>{t("company.logo.title")}</h2>
    {#if current?.logo}
      <img
        class="logo"
        src="/api/v1/organizations/{key}/logo?v={logoVersion}"
        alt={t("company.logo.alt", { name: current.name })}
      />
    {:else}
      <p class="muted hint">{t("company.logo.none")}</p>
    {/if}
    <label>
      {t("company.logo.choose")}
      <input
        type="file"
        accept="image/png,image/jpeg,image/gif,image/webp"
        onchange={(e) => {
          const file = e.currentTarget.files?.[0];
          if (file)
            void run(t("company.saved"), async () => {
              await uploadOrgLogo(key, file);
              logoVersion += 1;
              onchange();
            });
        }}
      />
    </label>
    {#if current?.logo}
      <div>
        <button
          type="button"
          onclick={() =>
            void run(t("company.saved"), async () => {
              await removeOrgLogo(key);
              onchange();
            })}>{t("company.logo.remove")}</button
        >
      </div>
    {/if}
  </section>
{:else if active === "support"}
  <form
    class="panel"
    onsubmit={(e) => {
      e.preventDefault();
      void run(t("company.support.granted"), async () => {
        await grantSupport(
          key,
          supportForever
            ? { level: supportLevel, until_revoked: true }
            : { level: supportLevel, hours: supportHours },
        );
        grants = await listSupportGrants(key);
      });
    }}
  >
    <h2>{t("company.support.title")}</h2>
    <p class="muted hint">{t("company.support.intro")}</p>
    <label>
      {t("company.support.level")}
      <select bind:value={supportLevel}>
        <option value="read_only">{t("company.support.readOnly")}</option>
        <option value="full">{t("company.support.full")}</option>
      </select>
    </label>
    <label class="check">
      <input type="checkbox" bind:checked={supportForever} />
      {t("company.support.untilRevoked")}
    </label>
    {#if !supportForever}
      <label>
        {t("company.support.hours")}
        <input type="number" min="1" max="720" bind:value={supportHours} />
      </label>
    {/if}
    <div>
      <button class="primary" type="submit">{t("company.support.grant")}</button
      >
    </div>
  </form>
  <section class="panel">
    <ul class="list">
      {#each grants as g (g.id)}
        <li class:off={!g.valid}>
          <div>
            <strong
              >{g.level === "full"
                ? t("company.support.full")
                : t("company.support.readOnly")}</strong
            >
            <span class="muted"
              >{when(g.granted_at)} → {when(g.expires_at)}{g.revoked_at
                ? ` · ${t("company.support.revokedAt")}`
                : ""}</span
            >
          </div>
          {#if g.valid}
            <button
              type="button"
              onclick={() =>
                void run(t("company.support.revoked"), async () => {
                  await revokeSupport(key, g.id);
                  grants = await listSupportGrants(key);
                })}>{t("company.support.revoke")}</button
            >
          {/if}
        </li>
      {:else}
        <li class="muted">{t("company.support.empty")}</li>
      {/each}
    </ul>
  </section>
{:else if active === "audit"}
  <section class="panel">
    <h2>{t("company.nav.audit")}</h2>
    <div class="scroll">
      <table>
        <thead>
          <tr>
            <th>{t("company.audit.time")}</th>
            <th>{t("company.audit.actor")}</th>
            <th>{t("company.audit.operation")}</th>
            <th>{t("company.audit.target")}</th>
          </tr>
        </thead>
        <tbody>
          {#each audit as a (a.id)}
            <tr>
              <td>{when(a.ts)}</td>
              <td>{a.actor}</td>
              <td>{a.operation}</td>
              <td>{a.target}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    {#if moreAudit && audit.length > 0}
      <div>
        <button
          type="button"
          onclick={() =>
            void run("", async () => {
              const next = await listOrgAudit(key, audit[audit.length - 1]?.id);
              moreAudit = next.length > 0;
              audit = [...audit, ...next];
            })}>{t("company.audit.more")}</button
        >
      </div>
    {/if}
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
  .logo {
    max-width: 160px;
    max-height: 160px;
    border-radius: var(--atlas-radius-sm);
    object-fit: contain;
  }
  .switch {
    max-width: 320px;
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
</style>
