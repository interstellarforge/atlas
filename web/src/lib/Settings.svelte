<script lang="ts">
  import {
    ApiError,
    changePassword,
    getPreferences,
    listSessions,
    type Preferences,
    revokeSession,
    type SessionInfo,
    setPreferences,
    updateProfile,
    type User,
  } from "./api";
  import Avatar from "./Avatar.svelte";
  import { t } from "./i18n";
  import PageHero from "./PageHero.svelte";
  import { account, cardFields, saveCardFields } from "./prefs.svelte";
  import { setThemeMode } from "./theme.svelte";
  import Tokens from "./Tokens.svelte";

  let {
    user,
    section,
    onuser,
  }: {
    user: User;
    section: string;
    onuser: (u: User) => void;
  } = $props();

  const sections = ["profile", "security", "tokens", "preferences"] as const;
  const NOTIFY = ["assigned", "mention", "blocker_done", "followed"] as const;
  const dateTime = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });

  let name = $derived(user.name); // writable derived: resets when the user changes
  let current = $state("");
  let next = $state("");
  let notice = $state("");
  let error = $state("");
  let sessions = $state<SessionInfo[]>([]);
  let prefs = $state<Preferences | undefined>();

  const active = $derived(
    (sections as readonly string[]).includes(section) ? section : "profile",
  );

  async function loadSessions(): Promise<void> {
    sessions = await listSessions();
  }

  $effect(() => {
    if (active === "security") void loadSessions();
    if (active === "preferences" && !prefs)
      void getPreferences().then((p) => (prefs = p));
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
      error = t("settings.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }

  function saveProfile(event: SubmitEvent): void {
    event.preventDefault();
    void run(t("settings.saved"), async () => {
      onuser(await updateProfile(name));
    });
  }

  function savePassword(event: SubmitEvent): void {
    event.preventDefault();
    void run(t("settings.passwordChanged"), async () => {
      await changePassword(current, next);
      current = "";
      next = "";
      await loadSessions();
    });
  }

  function savePrefs(): void {
    if (!prefs) return;
    const value = prefs;
    void run(t("settings.saved"), async () => {
      prefs = await setPreferences(value);
      setThemeMode(prefs.theme);
      account.defaultView = prefs.default_view;
    });
  }

  function toggleNotify(type: string, on: boolean): void {
    if (!prefs) return;
    prefs.muted_notifications = on
      ? prefs.muted_notifications.filter((n) => n !== type)
      : [...prefs.muted_notifications, type];
    savePrefs();
  }
</script>

<PageHero title={t("settings.title")} description={t("settings.intro")}>
  {#snippet icon()}<Avatar name={user.name} size={56} />{/snippet}
</PageHero>

<nav class="subnav" aria-label={t("settings.sections")}>
  {#each sections as s (s)}
    <a href="#/settings/{s}" aria-current={active === s ? "page" : undefined}
      >{t(`settings.nav.${s}` as "settings.nav.profile")}</a
    >
  {/each}
</nav>

{#if notice}<p class="banner ok" role="status">{notice}</p>{/if}
{#if error}<p class="banner error" role="alert">{error}</p>{/if}

{#if active === "profile"}
  <form class="panel" onsubmit={saveProfile}>
    <h2>{t("settings.nav.profile")}</h2>
    <label>
      {t("settings.name")}
      <input bind:value={name} required maxlength="100" />
    </label>
    <label>
      {t("settings.email")}
      <input value={user.email} readonly />
      <small class="muted">{t("settings.emailHint")}</small>
    </label>
    <div>
      <button class="primary" type="submit">{t("settings.save")}</button>
    </div>
  </form>
{:else if active === "security"}
  <form class="panel" onsubmit={savePassword}>
    <h2>{t("settings.password")}</h2>
    <label>
      {t("settings.currentPassword")}
      <input
        type="password"
        bind:value={current}
        autocomplete="current-password"
        required
      />
    </label>
    <label>
      {t("settings.newPassword")}
      <input
        type="password"
        bind:value={next}
        autocomplete="new-password"
        minlength="8"
        required
      />
    </label>
    <p class="muted hint">{t("settings.passwordHint")}</p>
    <div>
      <button class="primary" type="submit"
        >{t("settings.changePassword")}</button
      >
    </div>
  </form>
  <section class="panel" aria-labelledby="sessions-title">
    <h2 id="sessions-title">{t("settings.sessions")}</h2>
    <ul class="list">
      {#each sessions as s (s.id)}
        <li>
          <div>
            <strong
              >{t("settings.sessionStarted", {
                date: dateTime.format(s.created_at),
              })}</strong
            >
            <span class="muted"
              >{t("settings.sessionExpires", {
                date: dateTime.format(s.expires_at),
              })}</span
            >
          </div>
          {#if s.current}
            <span class="chip">{t("settings.thisSession")}</span>
          {:else}
            <button
              type="button"
              onclick={() =>
                void run(t("settings.sessionRevoked"), async () => {
                  await revokeSession(s.id);
                  await loadSessions();
                })}>{t("settings.revoke")}</button
            >
          {/if}
        </li>
      {/each}
    </ul>
  </section>
{:else if active === "tokens"}
  <Tokens embedded />
{:else if prefs}
  <section class="panel" aria-labelledby="prefs-title">
    <h2 id="prefs-title">{t("settings.nav.preferences")}</h2>
    <label>
      {t("settings.theme")}
      <select bind:value={prefs.theme} onchange={savePrefs}>
        {#each ["system", "light", "dark"] as m (m)}
          <option value={m}
            >{t(`settings.themes.${m}` as "settings.themes.system")}</option
          >
        {/each}
      </select>
    </label>
    <label>
      {t("settings.defaultView")}
      <select bind:value={prefs.default_view} onchange={savePrefs}>
        {#each ["board", "list", "backlog", "tree"] as v (v)}
          <option value={v}>{t(`project.${v}` as "project.board")}</option>
        {/each}
      </select>
    </label>
    <fieldset>
      <legend>{t("settings.cardFields")}</legend>
      <p class="muted hint">{t("settings.cardFieldsHint")}</p>
      {#each [["priority", "filters.showPriority"], ["assignee", "filters.showAssignee"], ["due", "filters.showDue"], ["labels", "filters.showLabels"]] as const as [field, label] (field)}
        <label class="check">
          <input
            type="checkbox"
            bind:checked={cardFields[field]}
            onchange={saveCardFields}
          />
          {t(label)}
        </label>
      {/each}
    </fieldset>
  </section>
  <section class="panel" aria-labelledby="notify-title">
    <h2 id="notify-title">{t("settings.notifications")}</h2>
    <p class="muted hint">{t("settings.notificationsHint")}</p>
    {#each NOTIFY as type (type)}
      <label class="check">
        <input
          type="checkbox"
          checked={!prefs.muted_notifications.includes(type)}
          onchange={(e) => {
            toggleNotify(type, e.currentTarget.checked);
          }}
        />
        {t(`settings.notify.${type}` as "settings.notify.assigned")}
      </label>
    {/each}
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
    max-width: 640px;
    padding: 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
  }
  h2 {
    margin: 0;
    font-size: var(--atlas-fs-lg);
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
    font-size: var(--atlas-fs-md);
    color: var(--color-text);
  }
  fieldset {
    display: grid;
    gap: var(--space-2);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
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
    justify-content: space-between;
    gap: var(--space-3);
    padding: 10px 0;
    border-top: 1px solid var(--color-border);
  }
  .list li:first-child {
    border-top: none;
  }
  .list li div {
    display: grid;
    gap: 2px;
  }
</style>
