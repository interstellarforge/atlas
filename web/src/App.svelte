<script lang="ts">
  import { login, logout, me, type User } from "./lib/api";
  import { ApiError } from "./lib/api";
  import { t } from "./lib/i18n";
  import { navigate, route } from "./lib/router.svelte";
  import { theme, toggleTheme } from "./lib/theme.svelte";

  let user = $state<User | undefined>();
  let ready = $state(false);
  let email = $state("");
  let password = $state("");
  let error = $state("");

  void me()
    .then((u) => (user = u))
    .finally(() => (ready = true));

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
        <a href="#/" aria-current={route.path === "/" ? "page" : undefined}
          >{t("nav.boards")}</a
        >
        <a
          href="#/tokens"
          aria-current={route.path === "/tokens" ? "page" : undefined}
          >{t("nav.tokens")}</a
        >
      </nav>
      <span class="spacer"></span>
      <button onclick={toggleTheme} aria-pressed={theme.current === "dark"}
        >{t("nav.toggleTheme")}</button
      >
      <button onclick={signOut}>{t("nav.signOut")}</button>
    </header>
    <main>
      {#if route.path === "/"}
        <h1>{t("home.title", { name: user.name })}</h1>
        <p class="muted">{t("home.empty")}</p>
      {:else if route.path === "/tokens"}
        <h1>{t("tokens.title")}</h1>
        <p class="muted">{t("tokens.empty")}</p>
      {:else}
        <h1>{t("notFound.title")}</h1>
        <p><a href="#/">{t("notFound.back")}</a></p>
      {/if}
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
  h1 {
    margin: 0;
    font-size: 1.25rem;
  }
  label {
    display: grid;
    gap: var(--space-1);
    font-size: 0.875rem;
    color: var(--color-muted);
  }
  input {
    padding: var(--space-2) var(--space-3);
    background: var(--color-bg);
    color: var(--color-text);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
  }
  button {
    padding: var(--space-2) var(--space-3);
    background: var(--color-surface);
    color: var(--color-text);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    cursor: pointer;
  }
  button.primary {
    background: var(--color-accent);
    color: var(--color-accent-text);
    border-color: var(--color-accent);
  }
  .error {
    margin: 0;
    color: var(--color-danger);
  }
  .muted {
    color: var(--color-muted);
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
  main {
    padding: var(--space-4);
  }
</style>
