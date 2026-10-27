<script lang="ts">
  import Admin from "./lib/Admin.svelte";
  import {
    ApiError,
    getPreferences,
    getPublicPolicy,
    listProjects,
    login,
    logout,
    me,
    type Project,
    type PublicPolicy,
    register,
    type User,
  } from "./lib/api";
  import Board from "./lib/Board.svelte";
  import Brand from "./lib/Brand.svelte";
  import { connect, disconnect, onChange } from "./lib/events.svelte";
  import ForcedPassword from "./lib/ForcedPassword.svelte";
  import HeroArt from "./lib/HeroArt.svelte";
  import { t } from "./lib/i18n";
  import Inbox from "./lib/Inbox.svelte";
  import PageHero from "./lib/PageHero.svelte";
  import { loadPeople } from "./lib/people.svelte";
  import { account } from "./lib/prefs.svelte";
  import Projects from "./lib/Projects.svelte";
  import ProjectView from "./lib/ProjectView.svelte";
  import { navigate, route } from "./lib/router.svelte";
  import { matchProject } from "./lib/routes";
  import Settings from "./lib/Settings.svelte";
  import Shell from "./lib/Shell.svelte";
  import { setThemeMode } from "./lib/theme.svelte";
  import { inbox, refreshInbox } from "./lib/unread.svelte";

  let user = $state<User | undefined>();
  let ready = $state(false);
  let email = $state("");
  let password = $state("");
  let error = $state("");
  let code = $state("");
  let needCode = $state(false);
  let policy = $state<PublicPolicy | undefined>();
  let regName = $state("");
  let regInvite = $state("");
  let notice = $state("");
  const registering = $derived(route.path.startsWith("/register"));
  let projects = $state<Project[]>([]);

  const current = $derived(matchProject(route.path));
  const allKeys = $derived(projects.map((p) => p.key));

  void me()
    .then((u) => (user = u))
    .finally(() => (ready = true));

  async function loadProjects(): Promise<void> {
    projects = (await listProjects()).filter((p) => !p.archived_at);
  }

  // Live updates and the people list only make sense while signed in.
  $effect(() => {
    if (user) {
      connect();
      void loadPeople();
      void refreshInbox();
      void loadProjects();
      void getPreferences().then((p) => {
        account.defaultView = p.default_view;
        if (p.theme !== "system") setThemeMode(p.theme);
      });
    } else {
      disconnect();
    }
  });

  // A notification signal is addressed to this user only; it updates the unread count.
  // Project changes refresh the sidebar list.
  $effect(() =>
    user
      ? onChange((event) => {
          if (event.type === "notification.created") void refreshInbox();
          if (event.type === "project.created") void loadProjects();
        })
      : undefined,
  );

  // Signed-in users have no use for the registration page.
  $effect(() => {
    if (user && route.path.startsWith("/register")) navigate("/");
  });

  // The token page moved into the personal settings.
  $effect(() => {
    if (route.path === "/tokens") navigate("/settings/tokens");
  });

  // The sign-in page offers registration only when the instance allows it.
  $effect(() => {
    if (ready && !user) void getPublicPolicy().then((p) => (policy = p));
  });

  // /register?invite=CODE pre-fills the invite code.
  $effect(() => {
    if (registering) {
      const q = route.path.split("?")[1] ?? "";
      regInvite = new URLSearchParams(q).get("invite") ?? regInvite;
    }
  });

  async function submitRegister(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = "";
    try {
      await register({
        email,
        name: regName,
        password,
        ...(regInvite && { invite: regInvite }),
      });
      user = await login(email, password);
      password = "";
      navigate("/");
    } catch (e) {
      error = t("auth.register.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = "";
    try {
      user = await login(email, password, code);
      password = "";
      code = "";
      needCode = false;
    } catch (e) {
      if (e instanceof ApiError && e.code === "totp_required") {
        needCode = true;
        notice = t("auth.login.codeNeeded");
      } else if (e instanceof ApiError && e.status === 429)
        error = t("auth.login.rateLimited");
      else if (e instanceof ApiError && e.status === 401)
        error = t("auth.login.invalid");
      else error = t("auth.login.failed");
    }
  }

  async function signOut(): Promise<void> {
    await logout();
    user = undefined;
    navigate("/");
  }
</script>

{#if !ready}
  <p class="loading muted">{t("app.loading")}</p>
{:else if !user}
  <div class="auth">
    <aside class="story" aria-hidden="true">
      <HeroArt kind="login" />
      <div class="shade"></div>
      <blockquote>
        <p>{t("auth.quote")}</p>
        <cite>{t("app.maker")}</cite>
      </blockquote>
    </aside>
    <main class="formside">
      {#if registering}
        <form onsubmit={submitRegister}>
          <div class="header">
            <Brand />
            <h1>{t("auth.register.title")}</h1>
            <p class="muted">
              {policy?.registration === "open"
                ? t("auth.register.open", {
                    domain: policy.allowed_domain || "*",
                  })
                : t("auth.register.invite")}
            </p>
          </div>
          <label
            >{t("settings.name")}<input
              bind:value={regName}
              required
              maxlength="100"
            /></label
          >
          <label>
            {t("auth.login.email")}
            <input
              type="email"
              bind:value={email}
              autocomplete="username"
              required
            />
          </label>
          <label>
            {t("auth.login.password")}
            <input
              type="password"
              bind:value={password}
              autocomplete="new-password"
              minlength={policy?.min_password_length ?? 8}
              required
            />
            <small class="muted"
              >{t("auth.register.minLength", {
                count: policy?.min_password_length ?? 8,
              })}</small
            >
          </label>
          {#if policy?.registration !== "open" || regInvite}
            <label
              >{t("auth.register.invitecode")}<input
                bind:value={regInvite}
                autocomplete="off"
              /></label
            >
          {/if}
          {#if error}<p class="banner error" role="alert">{error}</p>{/if}
          <button class="primary" type="submit"
            >{t("auth.register.submit")}</button
          >
          <a class="switch" href="#/">{t("auth.register.haveAccount")}</a>
        </form>
      {:else}
        <form onsubmit={submit}>
          <div class="header">
            <Brand />
            <h1>{t("auth.login.title")}</h1>
            <p class="muted">{t("auth.login.subtitle")}</p>
          </div>
          <label>
            {t("auth.login.email")}
            <input
              type="email"
              bind:value={email}
              autocomplete="username"
              required
            />
          </label>
          <label>
            {t("auth.login.password")}
            <input
              type="password"
              bind:value={password}
              autocomplete="current-password"
              required
            />
          </label>
          {#if needCode}
            <label>
              {t("auth.login.code")}
              <input
                bind:value={code}
                autocomplete="one-time-code"
                inputmode="numeric"
                required
              />
              <small class="muted">{t("auth.login.codeHint")}</small>
            </label>
            {#if notice && !error}<p class="banner" role="status">
                {notice}
              </p>{/if}
          {/if}
          {#if error}<p class="banner error" role="alert">{error}</p>{/if}
          <button class="primary" type="submit">{t("auth.login.submit")}</button
          >
          {#if policy && policy.registration !== "closed"}
            <a class="switch" href="#/register">{t("auth.register.create")}</a>
          {/if}
        </form>
      {/if}
    </main>
  </div>
{:else if user.must_change_password}
  <ForcedPassword
    {user}
    ondone={() => (user = user && { ...user, must_change_password: false })}
    onsignout={() => void signOut()}
  />
{:else}
  <Shell
    {user}
    {projects}
    unread={inbox.unread}
    onsignout={() => void signOut()}
  >
    {#if route.path === "/"}
      <Projects {projects} onchange={() => void loadProjects()} />
    {:else if current}
      <ProjectView
        project={current.key}
        name={projects.find((p) => p.key === current.key)?.name ?? ""}
        {user}
        repository={projects.find((p) => p.key === current.key)?.repository ??
          null}
        canAdmin={user.is_admin ||
          projects.find((p) => p.key === current.key)?.role === "admin"}
        view={current.view}
        section={current.section ?? ""}
        onchange={() => void loadProjects()}
        item={current.item}
      />
    {:else if route.path === "/inbox"}
      <Inbox />
    {:else if route.path === "/board"}
      <PageHero title={t("cross.title")} description={t("cross.intro")} />
      <div class="fill"><Board projects={allKeys} /></div>
    {:else if route.path.startsWith("/admin") && user.is_admin}
      <Admin {user} section={route.path.split("/")[2] ?? "users"} />
    {:else if route.path.startsWith("/settings")}
      <Settings
        {user}
        section={route.path.split("/")[2] ?? "profile"}
        onuser={(u: User) => (user = u)}
        ondeleted={() => {
          user = undefined;
          navigate("/");
        }}
      />
    {:else}
      <h1>{t("notFound.title")}</h1>
      <p><a href="#/">{t("notFound.back")}</a></p>
    {/if}
  </Shell>
{/if}

<style>
  .fill {
    flex: 1;
    min-height: 420px;
  }
  .loading {
    min-height: 100dvh;
    display: grid;
    place-items: center;
  }
  .auth {
    display: grid;
    grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr);
    min-height: 100dvh;
    background: var(--color-bg);
  }
  .story {
    position: relative;
    isolation: isolate;
    overflow: hidden;
    display: flex;
    align-items: flex-end;
    padding: 48px;
    background: linear-gradient(
      160deg,
      var(--atlas-hero-1),
      var(--atlas-hero-2)
    );
    color: var(--atlas-hero-text);
  }
  .shade {
    position: absolute;
    inset: 0;
    background: linear-gradient(0deg, rgb(4 10 22 / 70%), transparent 60%);
  }
  blockquote {
    position: relative;
    margin: 0;
    max-width: 24ch;
    font-size: 28px;
    line-height: 1.25;
    letter-spacing: -0.02em;
  }
  blockquote p {
    margin: 0 0 var(--space-3);
  }
  cite {
    font-size: var(--atlas-fs-xs);
    font-style: normal;
    font-weight: var(--atlas-fw-semibold);
    letter-spacing: 0.14em;
    text-transform: uppercase;
    color: #c5d6ea;
  }
  .formside {
    display: grid;
    place-items: center;
    padding: var(--space-6);
  }
  form {
    width: min(400px, 100%);
    display: grid;
    gap: var(--space-4);
  }
  .header {
    display: grid;
    gap: var(--space-3);
    margin-bottom: var(--space-2);
  }
  .header p {
    margin: 0;
  }
  form button {
    width: 100%;
    min-height: 44px;
  }
  form input {
    min-height: 44px;
  }
  .switch {
    text-align: center;
    font-size: var(--atlas-fs-sm);
  }
  label {
    display: grid;
    gap: 6px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
  }
  @media (max-width: 860px) {
    .auth {
      grid-template-columns: minmax(0, 1fr);
    }
    .story {
      display: none;
    }
  }
</style>
