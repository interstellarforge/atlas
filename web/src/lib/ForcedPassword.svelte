<script lang="ts">
  import { ApiError, changePassword, type User } from "./api";
  import { t } from "./i18n";

  let {
    user,
    ondone,
    onsignout,
  }: { user: User; ondone: () => void; onsignout: () => void } = $props();

  let current = $state("");
  let next = $state("");
  let error = $state("");

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = "";
    try {
      await changePassword(current, next);
      ondone();
    } catch (e) {
      error = t("settings.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }
</script>

<main class="forced">
  <form onsubmit={submit}>
    <h1>{t("forcedPassword.title")}</h1>
    <p class="muted">{t("forcedPassword.intro", { name: user.name })}</p>
    {#if error}<p class="banner error" role="alert">{error}</p>{/if}
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
    <div class="actions">
      <button class="primary" type="submit"
        >{t("settings.changePassword")}</button
      >
      <button type="button" onclick={onsignout}
        >{t("forcedPassword.signOut")}</button
      >
    </div>
  </form>
</main>

<style>
  .forced {
    display: grid;
    place-items: center;
    min-height: 100vh;
    padding: var(--space-4);
  }
  form {
    display: grid;
    gap: var(--space-3);
    width: min(100%, 24rem);
  }
  label {
    display: grid;
    gap: var(--space-1);
  }
  .actions {
    display: flex;
    gap: var(--space-2);
  }
</style>
