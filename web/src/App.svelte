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
  import { connect, disconnect, realtime } from "./lib/events.svelte";
  import { t } from "./lib/i18n";
  import { loadPeople } from "./lib/people.svelte";
  import Projects from "./lib/Projects.svelte";
  import ProjectView from "./lib/ProjectView.svelte";
  import { navigate, route } from "./lib/router.svelte";
  import { matchProject } from "./lib/routes";
  import { theme, toggleTheme } from "./lib/theme.svelte";

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
      void listProjects().then((ps) => (allProjects = ps.map((p) => p.key)));
    } else {
      disconnect();
    }
  });

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
      <h1>{t("auth.login.title")}</h1>
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
      {#if error}<p class="error" role="alert">{error}</p>{/if}
      <button class="primary" type="submit">{t("auth.login.submit")}</button>
    </form>
  </main>
{:else}
  <div class="shell">
    <header>
      <strong>{t("app.name")}</strong>
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
          href="#/tokens"
          aria-current={route.path === "/tokens" ? "page" : undefined}
          >{t("nav.tokens")}</a
        >
      </nav>
      <span class="spacer"></span>
      <span class="live" class:off={!realtime.connected} role="status">
        <span aria-hidden="true">●</span>
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
          <ProjectView project={current.key} view={current.view} />
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
    padding: var(--space-4);
  }
  .card {
    width: min(24rem, 100%);
    display: grid;
    gap: var(--space-4);
    padding: var(--space-6);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
  }
  label {
    display: grid;
    gap: var(--space-1);
    font-size: 0.875rem;
    color: var(--color-muted);
  }
  .error {
    margin: 0;
  }
  .shell {
    display: flex;
    flex-direction: column;
    height: 100dvh;
  }
  header {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    background: var(--color-surface);
    border-bottom: 1px solid var(--color-border);
  }
  nav {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
  nav a {
    color: var(--color-muted);
    text-decoration: none;
  }
  nav a[aria-current="page"] {
    color: var(--color-text);
    font-weight: 600;
  }
  .spacer {
    flex: 1;
  }
  .live {
    color: #2e9e5b;
    font-size: 0.8rem;
  }
  .live.off {
    color: var(--color-muted);
  }
  main {
    flex: 1;
    min-height: 0;
    padding: var(--space-4);
  }
  .page {
    height: 100%;
    min-height: 0;
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
</style>
