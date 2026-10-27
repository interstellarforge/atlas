<script lang="ts">
  import {
    ApiError,
    listProjects,
    login,
    logout,
    me,
    type User,
  } from "./lib/api";
  import Board from "./lib/Board.svelte";
  import Brand from "./lib/Brand.svelte";
  import { connect, disconnect, onChange, realtime } from "./lib/events.svelte";
  import { t } from "./lib/i18n";
  import Inbox from "./lib/Inbox.svelte";
  import { loadPeople } from "./lib/people.svelte";
  import Projects from "./lib/Projects.svelte";
  import ProjectView from "./lib/ProjectView.svelte";
  import { navigate, route } from "./lib/router.svelte";
  import { matchProject } from "./lib/routes";
  import { theme, toggleTheme } from "./lib/theme.svelte";
  import { inbox, refreshInbox } from "./lib/unread.svelte";

  let user = $state<User | undefined>();
  let ready = $state(false);
  let email = $state("");
  let password = $state("");
  let error = $state("");
  let allProjects = $state<string[]>([]);

  const current = $derived(matchProject(route.path));

  void me()
    .then((u) => (user = u))
    .finally(() => (ready = true));

  // Live updates and the people list only make sense while signed in.
  $effect(() => {
    if (user) {
      connect();
      void loadPeople();
      void refreshInbox();
      void listProjects().then((ps) => (allProjects = ps.map((p) => p.key)));
    } else {
      disconnect();
    }
  });

  // A notification signal is addressed to this user only; it updates the unread count.
  $effect(() =>
    user
      ? onChange((event) => {
          if (event.type === "notification.created") void refreshInbox();
        })
      : undefined,
  );

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = "";
    try {
      user = await login(email, password);
      password = "";
    } catch (e) {
      if (e instanceof ApiError && e.status === 429)
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
  <p class="center muted">{t("app.loading")}</p>
{:else if !user}
  <main class="center">
    <form class="card" onsubmit={submit}>
      <div class="auth-header">
        <Brand />
        <h1>{t("auth.login.title")}</h1>
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
      {#if error}<p class="banner error" role="alert">{error}</p>{/if}
      <button class="primary" type="submit">{t("auth.login.submit")}</button>
    </form>
  </main>
{:else}
  <div class="shell">
    <header>
      <Brand />
      <nav aria-label={t("nav.main")}>
        <a
          href="#/"
          aria-current={route.path === "/" || current ? "page" : undefined}
          >{t("nav.projects")}</a
        >
        <a
          href="#/board"
          aria-current={route.path === "/board" ? "page" : undefined}
          >{t("nav.overview")}</a
        >
        <a
          href="#/inbox"
          aria-current={route.path === "/inbox" ? "page" : undefined}
          >{t("nav.inbox")}{#if inbox.unread > 0}
            <span
              class="badge"
              aria-label={t("nav.unread", { count: inbox.unread })}
              >{inbox.unread}</span
            >{/if}</a
        >
        <a
          href="#/tokens"
          aria-current={route.path === "/tokens" ? "page" : undefined}
          >{t("nav.tokens")}</a
        >
      </nav>
      <span class="spacer"></span>
      <span class="live" class:off={!realtime.connected} role="status">
        <span aria-hidden="true" class="dot">●</span>
        <span class="sr-only"
          >{realtime.connected ? t("nav.live") : t("nav.offline")}</span
        >
      </span>
      <button onclick={toggleTheme} aria-pressed={theme.current === "dark"}
        >{t("nav.toggleTheme")}</button
      >
      <button onclick={signOut}>{t("nav.signOut")}</button>
    </header>
    <main>
      <div class="page">
        {#if route.path === "/"}
          <Projects />
        {:else if current}
          <ProjectView
            project={current.key}
            view={current.view}
            item={current.item}
          />
        {:else if route.path === "/inbox"}
          <Inbox />
        {:else if route.path === "/board"}
          <h1>{t("cross.title")}</h1>
          <Board projects={allProjects} />
        {:else if route.path === "/tokens"}
          <h1>{t("tokens.title")}</h1>
          <p class="muted">{t("tokens.empty")}</p>
        {:else}
          <h1>{t("notFound.title")}</h1>
          <p><a href="#/">{t("notFound.back")}</a></p>
        {/if}
      </div>
    </main>
  </div>
{/if}

<style>
  .center {
    min-height: 100dvh;
    display: grid;
    place-items: center;
    padding: var(--space-6);
    background:
      radial-gradient(
        ellipse at 80% 75%,
        color-mix(in srgb, var(--color-accent) 17%, transparent),
        transparent 42%
      ),
      var(--color-bg);
  }
  .card {
    width: min(430px, 100%);
    display: grid;
    gap: var(--space-4);
    padding: 32px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-xl);
    box-shadow: var(--atlas-shadow-lg);
  }
  .auth-header {
    display: grid;
    justify-items: center;
    gap: var(--space-3);
    text-align: center;
  }
  .card button {
    width: 100%;
  }
  label {
    display: grid;
    gap: 6px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
  }
  .shell {
    display: flex;
    flex-direction: column;
    height: 100dvh;
  }
  header {
    flex: none;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3) 22px;
    min-height: var(--atlas-topbar-h);
    padding: var(--space-2) 28px;
    background: var(--color-surface);
    border-bottom: 1px solid var(--color-border);
  }
  nav {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
  }
  nav a {
    padding: 10px 13px;
    border-radius: var(--radius);
    color: var(--color-muted);
    font-weight: var(--atlas-fw-semibold);
    text-decoration: none;
    white-space: nowrap;
  }
  nav a:hover {
    background: var(--color-soft);
  }
  nav a[aria-current="page"] {
    color: var(--color-accent);
    background: var(--color-accent-soft);
  }
  .badge {
    display: inline-block;
    min-width: 18px;
    margin-left: 6px;
    padding: 0 5px;
    border-radius: var(--atlas-radius-pill);
    background: var(--color-accent);
    color: var(--color-accent-text);
    font-size: var(--atlas-fs-xs);
    font-weight: var(--atlas-fw-bold);
    text-align: center;
  }
  .spacer {
    flex: 1;
  }
  .live {
    display: inline-flex;
    align-items: center;
    color: var(--color-success);
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
  }
  .live.off {
    color: var(--color-warning);
  }
  .dot {
    font-size: 0.7rem;
  }
  main {
    flex: 1;
    min-height: 0;
    padding: 28px;
  }
  .page {
    height: 100%;
    min-height: 0;
    max-width: 1600px;
    margin: 0 auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    overflow-y: auto;
  }
  .page > :global(.board),
  .page > :global(.project) {
    flex: 1;
    min-height: 0;
  }
  @media (max-width: 900px) {
    header {
      padding: var(--space-2) 16px;
    }
    main {
      padding: 20px 16px;
    }
  }
  @media (max-width: 600px) {
    header {
      padding: var(--space-2) 12px;
      gap: var(--space-2);
    }
    nav a {
      padding: 9px;
      font-size: var(--atlas-fs-sm);
    }
    main {
      padding: 14px 12px 24px;
    }
  }
</style>
