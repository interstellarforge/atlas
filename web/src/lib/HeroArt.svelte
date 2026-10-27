<script lang="ts">
  import hero from "../assets/atlas/hero.webp";
  import login from "../assets/atlas/login.webp";
  import sidebar from "../assets/atlas/sidebar.webp";

  // Decorative artwork behind white overlay text, so the dark variant is used in both themes.
  const art = {
    hero: { src: hero, width: 1400, height: 350 },
    login: { src: login, width: 1600, height: 2000 },
    sidebar: { src: sidebar, width: 800, height: 600 },
  };
  let { kind = "hero" }: { kind?: keyof typeof art } = $props();
  const { src, width, height } = $derived(art[kind]);
</script>

<img
  class="art"
  {src}
  {width}
  {height}
  alt=""
  decoding="async"
  loading={kind === "login" ? "eager" : "lazy"}
/>

<style>
  .art {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    object-fit: cover;
    object-position: right bottom;
    pointer-events: none;
  }
</style>
