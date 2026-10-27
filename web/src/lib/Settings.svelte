<script lang="ts">
  import {
    ApiError,
    changeEmail,
    changePassword,
    deleteAccount,
    getPreferences,
    getSecurity,
    listSessions,
    type Preferences,
    removeAvatar,
    revokeSession,
    type SecurityInfo,
    type SessionInfo,
    setPreferences,
    totpDisable,
    totpEnable,
    totpSetup,
    updateProfile,
    uploadAvatar,
    type User,
  } from "./api";
  import Avatar from "./Avatar.svelte";
  import { t } from "./i18n";
  import PageHero from "./PageHero.svelte";
  import { loadPeople } from "./people.svelte";
  import { account, cardFields, saveCardFields } from "./prefs.svelte";
  import { setThemeMode } from "./theme.svelte";
  import Tokens from "./Tokens.svelte";

  let {
    user,
    section,
    onuser,
    ondeleted,
  }: {
    user: User;
    section: string;
    onuser: (u: User) => void;
    ondeleted: () => void;
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
  let newEmail = $state("");
  let emailPassword = $state("");
  let sec = $state<SecurityInfo | undefined>();
  let enrol = $state<{ secret: string; uri: string } | undefined>();
  let enrolCode = $state("");
  let recovery = $state<string[]>([]);
  let offPassword = $state("");
  let offCode = $state("");
  let delPassword = $state("");
  let delConfirm = $state("");

  const active = $derived(
    (sections as readonly string[]).includes(section) ? section : "profile",
  );

  async function loadSessions(): Promise<void> {
    sessions = await listSessions();
  }

  $effect(() => {
    if (active === "security") {
      void loadSessions();
      void getSecurity().then((v) => (sec = v));
    }
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

  async function pickAvatar(event: Event): Promise<void> {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    await run(t("settings.avatarSaved"), async () => {
      await uploadAvatar(file);
      await loadPeople();
      onuser({ ...user, avatar: true });
    });
    input.value = "";
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
  {#snippet icon()}<Avatar
      name={user.name}
      size={56}
      userId={user.id}
    />{/snippet}
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
  <section class="panel" aria-labelledby="avatar-title">
    <h2 id="avatar-title">{t("settings.avatar")}</h2>
    <div class="avrow">
      <Avatar name={user.name} size={72} userId={user.id} />
      <div class="stack">
        <input
          type="file"
          accept="image/png,image/jpeg,image/webp"
          aria-label={t("settings.avatar")}
          onchange={(e) => void pickAvatar(e)}
        />
        <p class="muted hint">{t("settings.avatarHint")}</p>
        {#if user.avatar}
          <div>
            <button
              type="button"
              onclick={() =>
                void run(t("settings.avatarRemoved"), async () => {
                  await removeAvatar();
                  await loadPeople();
                  onuser({ ...user, avatar: false });
                })}>{t("settings.avatarRemove")}</button
            >
          </div>
        {/if}
      </div>
    </div>
  </section>
  <form
    class="panel"
    onsubmit={(e) => {
      e.preventDefault();
      void run(t("settings.emailChanged"), async () => {
        onuser(await changeEmail(newEmail, emailPassword));
        newEmail = "";
        emailPassword = "";
      });
    }}
  >
    <h2>{t("settings.changeEmail")}</h2>
    <p class="muted hint">{t("settings.emailChangeHint")}</p>
    <label>
      {t("settings.newEmail")}
      <input type="email" bind:value={newEmail} required />
    </label>
    <label>
      {t("settings.currentPassword")}
      <input
        type="password"
        bind:value={emailPassword}
        autocomplete="current-password"
        required
      />
    </label>
    <div>
      <button class="primary" type="submit">{t("settings.changeEmail")}</button>
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
  <section class="panel" aria-labelledby="totp-title">
    <h2 id="totp-title">{t("settings.twoFactor")}</h2>
    {#if recovery.length > 0}
      <div class="banner ok" role="status">
        <p>{t("settings.recoveryOnce")}</p>
        <ul class="codes">
          {#each recovery as c (c)}<li><code>{c}</code></li>{/each}
        </ul>
        <button type="button" onclick={() => (recovery = [])}
          >{t("settings.recoverySaved")}</button
        >
      </div>
    {/if}
    {#if sec?.totp}
      <p>
        <span class="chip">{t("settings.twoFactorOn")}</span>
        <span class="muted"
          >{t("settings.recoveryLeft", { count: sec.recovery_remaining })}</span
        >
      </p>
      <form
        class="stack"
        onsubmit={(e) => {
          e.preventDefault();
          void run(t("settings.twoFactorOff"), async () => {
            await totpDisable(offPassword, offCode);
            offPassword = "";
            offCode = "";
            sec = await getSecurity();
          });
        }}
      >
        <p class="muted hint">{t("settings.disableHint")}</p>
        <label
          >{t("settings.currentPassword")}<input
            type="password"
            bind:value={offPassword}
            required
            autocomplete="current-password"
          /></label
        >
        <label
          >{t("auth.login.code")}<input
            bind:value={offCode}
            required
            autocomplete="off"
          /></label
        >
        <div><button type="submit">{t("settings.disable")}</button></div>
      </form>
    {:else if enrol}
      <p class="muted hint">{t("settings.enrolHint")}</p>
      <label
        >{t("settings.secretKey")}<input
          value={enrol.secret}
          readonly
          aria-label={t("settings.secretKey")}
        /></label
      >
      <p class="hint"><a href={enrol.uri}>{t("settings.openInApp")}</a></p>
      <form
        class="stack"
        onsubmit={(e) => {
          e.preventDefault();
          void run(t("settings.twoFactorEnabled"), async () => {
            recovery = await totpEnable(enrolCode);
            enrol = undefined;
            enrolCode = "";
            sec = await getSecurity();
          });
        }}
      >
        <label
          >{t("settings.confirmCode")}<input
            bind:value={enrolCode}
            inputmode="numeric"
            autocomplete="one-time-code"
            required
          /></label
        >
        <div>
          <button class="primary" type="submit">{t("settings.enable")}</button>
        </div>
      </form>
    {:else}
      <p class="muted hint">{t("settings.twoFactorIntro")}</p>
      <div>
        <button
          type="button"
          class="primary"
          onclick={() =>
            void run("", async () => {
              enrol = await totpSetup();
            })}>{t("settings.setupTwoFactor")}</button
        >
      </div>
    {/if}
  </section>
  <form
    class="panel danger"
    onsubmit={(e) => {
      e.preventDefault();
      if (delConfirm !== user.email) return;
      void run("", async () => {
        await deleteAccount(delPassword);
        ondeleted();
      });
    }}
  >
    <h2>{t("settings.deleteAccount")}</h2>
    <p class="muted hint">{t("settings.deleteHint")}</p>
    <label
      >{t("settings.currentPassword")}<input
        type="password"
        bind:value={delPassword}
        required
        autocomplete="current-password"
      /></label
    >
    <label
      >{t("settings.typeEmail", { email: user.email })}<input
        bind:value={delConfirm}
        autocomplete="off"
      /></label
    >
    <div>
      <button
        type="submit"
        class="dangerbtn"
        disabled={delConfirm !== user.email}
        >{t("settings.deleteAccount")}</button
      >
    </div>
  </form>
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
    <label class="check">
      <input
        type="checkbox"
        bind:checked={prefs.email_notifications}
        onchange={savePrefs}
      />
      {t("settings.emailNotifications")}
    </label>
    <p class="muted hint">{t("settings.emailNotificationsHint")}</p>
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
  .panel.danger {
    border-color: var(--color-danger);
  }
  .dangerbtn {
    color: var(--color-danger);
    border-color: var(--color-danger);
  }
  .avrow {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }
  .stack {
    display: grid;
    gap: var(--space-3);
  }
  .codes {
    columns: 2;
    list-style: none;
    margin: 0 0 var(--space-2);
    padding: 0;
    font-family: var(--font-mono);
  }
  .list li div {
    display: grid;
    gap: 2px;
  }
</style>
