<script lang="ts">
  import { type Snippet, tick } from "svelte";

  import type { Project, User } from "./api";
  import Avatar from "./Avatar.svelte";
  import Brand from "./Brand.svelte";
  import { realtime } from "./events.svelte";
  import HeroArt from "./HeroArt.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import type { IconName } from "./icons";
  import { layout, toggleSidebar } from "./prefs.svelte";
  import { projectColor } from "./projectColor";
  import { route } from "./router.svelte";
  import { matchProject } from "./routes";
  import { theme, toggleTheme } from "./theme.svelte";

  let {
    user,
    projects,
    unread,
    onsignout,
    children,
  }: {
    user: User;
    projects: Project[];
    unread: number;
    onsignout: () => void;
    children: Snippet;
  } = $props();

  let drawerOpen = $state(false);
  let drawerEl = $state<HTMLElement | undefined>();
  let menuButton = $state<HTMLButtonElement | undefined>();

  const current = $derived(matchProject(route.path));
  const links: {
    href: string;
    path: string;
    icon: IconName;
    label: "nav.projects" | "nav.overview" | "nav.inbox" | "nav.tokens";
  }[] = [
    { href: "#/", path: "/", icon: "projects", label: "nav.projects" },
    {
      href: "#/board",
      path: "/board",
      icon: "overview",
      label: "nav.overview",
    },
    { href: "#/inbox", path: "/inbox", icon: "inbox", label: "nav.inbox" },
    { href: "#/tokens", path: "/tokens", icon: "key", label: "nav.tokens" },
  ];

  const viewLabel = {
    board: "project.board",
    list: "project.list",
    backlog: "project.backlog",
    tree: "project.tree",
    milestones: "project.milestones",
    settings: "project.settings",
  } as const;

  const crumbs = $derived.by(() => {
    if (current)
      return [
        { label: t("nav.projects"), href: "#/" },
        { label: current.key, href: `#/p/${current.key}` },
        { label: t(viewLabel[current.view]) },
      ];
    const link = links.find((l) => l.path === route.path);
    return [{ label: link ? t(link.label) : t("notFound.title") }];
  });

  // The drawer closes whenever the route changes, so a tapped link never leaves it open.
  let lastPath = route.path;
  $effect(() => {
    if (route.path === lastPath) return;
    lastPath = route.path;
    drawerOpen = false;
  });

  async function openDrawer(): Promise<void> {
    drawerOpen = true;
    await tick();
    drawerEl?.querySelector<HTMLElement>("a, button")?.focus();
  }

  function closeDrawer(): void {
    drawerOpen = false;
    menuButton?.focus();
  }

  // Escape closes; Tab stays inside the open drawer.
  function onDrawerKey(event: KeyboardEvent): void {
    if (event.key === "Escape") {
      closeDrawer();
      return;
    }
    if (event.key !== "Tab" || !drawerEl) return;
    const focusable = [
      ...drawerEl.querySelectorAll<HTMLElement>(
        "a[href], button:not(:disabled)",
      ),
    ];
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last?.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first?.focus();
    }
  }
</script>

{#snippet sidebarBody(collapsed: boolean, inDrawer: boolean)}
  <div class="brandrow">
    <a
      class="brandlink"
      href="#/"
      aria-label={collapsed ? t("app.name") : undefined}
    >
      <Brand compact={collapsed} />
    </a>
    {#if inDrawer}
      <button
        type="button"
        class="iconbtn"
        aria-label={t("nav.closeMenu")}
        onclick={closeDrawer}><Icon name="close" /></button
      >
    {:else}
      <button
        type="button"
        class="iconbtn collapse"
        aria-label={collapsed ? t("nav.expand") : t("nav.collapse")}
        aria-expanded={!collapsed}
        onclick={toggleSidebar}
        ><Icon name={collapsed ? "chevrons-right" : "chevrons-left"} /></button
      >
    {/if}
  </div>

  <nav aria-label={t("nav.main")} class="main">
    {#each links as link (link.path)}
      <a
        href={link.href}
        class="navlink"
        title={collapsed ? t(link.label) : undefined}
        aria-current={route.path === link.path || (link.path === "/" && current)
          ? "page"
          : undefined}
      >
        <Icon name={link.icon} />
        <span class="label" class:sr-only={collapsed}>{t(link.label)}</span>
        {#if link.path === "/inbox" && unread > 0}
          <span class="badge" aria-label={t("nav.unread", { count: unread })}
            >{unread}</span
          >
        {/if}
      </a>
    {/each}
  </nav>

  <div class="section">
    <p class="heading" class:sr-only={collapsed}>{t("nav.workspace")}</p>
    <ul class="projects">
      {#each projects as project (project.key)}
        <li>
          <a
            href="#/p/{project.key}"
            class="projlink"
            title={collapsed ? project.name : undefined}
            aria-current={current?.key === project.key ? "page" : undefined}
          >
            <span class="tile" style:--accent={projectColor(project.key)}
              >{project.key.slice(0, 1)}</span
            >
            <span class="projtext" class:sr-only={collapsed}>
              <span class="projname">{project.name}</span>
              <span class="projkey">{project.key}</span>
            </span>
          </a>
        </li>
      {/each}
    </ul>
    {#if !collapsed}
      <a class="viewall" href="#/"
        >{t("nav.viewAll")} <Icon name="chevron-right" size={14} /></a
      >
    {/if}
  </div>

  <div class="maker" class:hidden={collapsed}>
    <HeroArt />
    <p>
      <strong>{t("app.maker")}</strong>
      <span>{t("app.tagline")}</span>
    </p>
  </div>

  <div class="account">
    <Avatar name={user.name} size={32} />
    <span class="who" class:sr-only={collapsed}>
      <span class="whoname">{user.name}</span>
      <span class="whoemail">{user.email}</span>
    </span>
    <button
      type="button"
      class="iconbtn"
      aria-label={t("nav.signOut")}
      title={t("nav.signOut")}
      onclick={onsignout}><Icon name="logout" /></button
    >
  </div>
{/snippet}

<div class="shell" class:collapsed={layout.collapsed}>
  <aside class="sidebar" aria-label={t("nav.sidebar")}>
    {@render sidebarBody(layout.collapsed, false)}
  </aside>

  <div class="column">
    <header class="topbar">
      <button
        type="button"
        class="iconbtn menu"
        bind:this={menuButton}
        aria-label={t("nav.openMenu")}
        aria-expanded={drawerOpen}
        onclick={() => void openDrawer()}><Icon name="menu" size={20} /></button
      >
      <nav class="crumbs" aria-label={t("nav.breadcrumb")}>
        {#each crumbs as crumb, i (i)}
          {#if i > 0}<Icon name="chevron-right" size={14} />{/if}
          {#if crumb.href && i < crumbs.length - 1}
            <a href={crumb.href}>{crumb.label}</a>
          {:else}
            <span aria-current="page">{crumb.label}</span>
          {/if}
        {/each}
      </nav>
      <span class="spacer"></span>
      <span class="live" class:off={!realtime.connected} role="status">
        <span aria-hidden="true" class="dot"></span>
        <span class="livetext"
          >{realtime.connected ? t("nav.live") : t("nav.offline")}</span
        >
      </span>
      <button
        type="button"
        class="iconbtn"
        aria-label={t("nav.toggleTheme")}
        title={t("nav.toggleTheme")}
        aria-pressed={theme.current === "dark"}
        onclick={toggleTheme}
        ><Icon name={theme.current === "dark" ? "sun" : "moon"} /></button
      >
    </header>
    <main>
      {@render children()}
    </main>
  </div>
</div>

{#if drawerOpen}
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="backdrop" onclick={closeDrawer}></div>
  <div
    class="drawer"
    role="dialog"
    aria-modal="true"
    aria-label={t("nav.main")}
    tabindex="-1"
    bind:this={drawerEl}
    onkeydown={onDrawerKey}
  >
    {@render sidebarBody(false, true)}
  </div>
{/if}

<style>
  .shell {
    display: grid;
    grid-template-columns: var(--atlas-sidebar-w) minmax(0, 1fr);
    height: 100dvh;
    transition: grid-template-columns var(--atlas-duration) var(--atlas-ease);
  }
  .shell.collapsed {
    grid-template-columns: var(--atlas-sidebar-w-collapsed) minmax(0, 1fr);
  }
  .sidebar,
  .drawer {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    min-height: 0;
    padding: 16px 12px;
    background: var(--atlas-sidebar);
    border-right: 1px solid var(--color-border);
    overflow-y: auto;
  }
  .brandrow {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2);
    padding: 0 4px;
  }
  .brandlink {
    text-decoration: none;
    min-width: 0;
  }
  .iconbtn {
    flex: none;
    display: inline-grid;
    place-items: center;
    width: 36px;
    min-height: 36px;
    height: 36px;
    padding: 0;
    color: var(--color-muted);
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--atlas-radius-sm);
  }
  .iconbtn:hover:not(:disabled) {
    color: var(--color-text);
    background: var(--color-soft);
    border-color: var(--color-border);
  }
  .main {
    display: grid;
    gap: 2px;
  }
  .navlink,
  .projlink {
    position: relative;
    display: flex;
    align-items: center;
    gap: 12px;
    min-height: 40px;
    padding: 8px 12px;
    border-radius: var(--atlas-radius-sm);
    color: var(--color-muted);
    font-weight: var(--atlas-fw-semibold);
    text-decoration: none;
    transition:
      background var(--atlas-duration) var(--atlas-ease),
      color var(--atlas-duration) var(--atlas-ease);
  }
  .navlink:hover,
  .projlink:hover {
    background: var(--color-soft);
    color: var(--color-text);
  }
  .navlink[aria-current="page"],
  .projlink[aria-current="page"] {
    background: var(--color-accent-soft);
    color: var(--color-accent-soft-text);
  }
  .navlink[aria-current="page"]::before,
  .projlink[aria-current="page"]::before {
    content: "";
    position: absolute;
    left: -12px;
    top: 8px;
    bottom: 8px;
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--atlas-primary);
  }
  .collapsed .navlink,
  .collapsed .projlink {
    justify-content: center;
    padding: 8px;
  }
  .badge {
    margin-left: auto;
    min-width: 22px;
    padding: 1px 7px;
    border-radius: var(--atlas-radius-pill);
    background: var(--color-soft);
    border: 1px solid var(--color-border);
    color: var(--color-text);
    font-size: var(--atlas-fs-xs);
    font-weight: var(--atlas-fw-bold);
    text-align: center;
  }
  .collapsed .badge {
    position: absolute;
    top: 2px;
    right: 4px;
    min-width: 0;
    padding: 0 5px;
    background: var(--atlas-primary);
    color: var(--atlas-primary-on);
    border: none;
  }
  .section {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding-top: var(--space-4);
    border-top: 1px solid var(--color-border);
  }
  .heading {
    margin: 0;
    padding: 0 12px;
    font-size: var(--atlas-fs-xs);
    font-weight: var(--atlas-fw-bold);
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--atlas-text-subtle);
  }
  .projects {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: 2px;
    overflow-y: auto;
    min-height: 0;
  }
  .tile {
    flex: none;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    border-radius: 7px;
    font-size: 12px;
    font-weight: var(--atlas-fw-bold);
    color: #06101f;
    background: var(--accent);
  }
  .projtext,
  .who {
    display: grid;
    min-width: 0;
    line-height: 1.25;
  }
  .projname,
  .whoname {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text);
    font-size: var(--atlas-fs-md);
  }
  .projkey,
  .whoemail {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font: var(--atlas-fw-regular) var(--atlas-fs-xs) var(--font-mono);
    color: var(--atlas-text-subtle);
  }
  .viewall {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 6px 12px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
    text-decoration: none;
  }
  .viewall:hover {
    color: var(--color-accent);
  }
  .maker {
    position: relative;
    isolation: isolate;
    overflow: hidden;
    flex: none;
    min-height: 84px;
    display: flex;
    align-items: flex-end;
    padding: 10px 12px;
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
    background: linear-gradient(
      160deg,
      var(--atlas-hero-1),
      var(--atlas-hero-2)
    );
    color: var(--atlas-hero-text);
  }
  .maker.hidden {
    display: none;
  }
  .maker p {
    position: relative;
    margin: 0;
    display: grid;
    line-height: 1.25;
    font-size: var(--atlas-fs-sm);
  }
  .maker strong {
    font-size: var(--atlas-fs-md);
  }
  .maker span {
    color: #c5d6ea;
  }
  .account {
    flex: none;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 4px 0;
    border-top: 1px solid var(--color-border);
  }
  .collapsed .account {
    flex-direction: column;
  }
  .who {
    flex: 1;
  }
  .column {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
  }
  .topbar {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--space-3);
    height: var(--atlas-topbar-h);
    padding: 0 28px;
    background: color-mix(in srgb, var(--color-bg) 88%, transparent);
    border-bottom: 1px solid var(--color-border);
  }
  .menu {
    display: none;
  }
  .crumbs {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    font-size: var(--atlas-fs-md);
    color: var(--color-muted);
    overflow: hidden;
    white-space: nowrap;
  }
  .crumbs a {
    color: var(--color-muted);
    text-decoration: none;
  }
  .crumbs a:hover {
    color: var(--color-text);
  }
  .crumbs [aria-current="page"] {
    color: var(--color-text);
    font-weight: var(--atlas-fw-semibold);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .spacer {
    flex: 1;
  }
  .live {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--color-success);
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
  }
  .live.off {
    color: var(--color-warning);
  }
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: currentcolor;
    box-shadow: 0 0 0 3px color-mix(in srgb, currentcolor 22%, transparent);
  }
  main {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 24px 28px 20px;
    gap: var(--space-4);
    overflow-y: auto;
  }
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: var(--atlas-overlay);
  }
  .drawer {
    position: fixed;
    z-index: 41;
    inset: 0 auto 0 0;
    width: min(320px, 86vw);
    box-shadow: var(--atlas-shadow-lg);
  }
  @media (max-width: 900px) {
    .shell,
    .shell.collapsed {
      grid-template-columns: minmax(0, 1fr);
    }
    .sidebar {
      display: none;
    }
    .menu {
      display: inline-grid;
    }
    .topbar {
      padding: 0 12px;
    }
    main {
      padding: 16px 12px 20px;
    }
  }
  @media (max-width: 600px) {
    .livetext {
      position: absolute;
      width: 1px;
      height: 1px;
      overflow: hidden;
      clip-path: inset(50%);
    }
  }
</style>
