<script lang="ts">
  // Planetary horizon drawn in SVG: no bitmap, no animation, a few hundred bytes. A source artwork
  // can replace it later; see docs/design/README.md (hero artwork).
</script>

<svg
  class="art"
  viewBox="0 0 900 220"
  preserveAspectRatio="xMaxYMax slice"
  aria-hidden="true"
  focusable="false"
>
  <defs>
    <radialGradient id="hero-glow" cx="78%" cy="115%" r="60%">
      <stop offset="0" stop-color="#4aa0ff" stop-opacity="0.55" />
      <stop offset="0.45" stop-color="#1b5fc4" stop-opacity="0.18" />
      <stop offset="1" stop-color="#1b5fc4" stop-opacity="0" />
    </radialGradient>
    <linearGradient id="hero-rim" x1="0" y1="0" x2="1" y2="0">
      <stop offset="0" stop-color="#4aa0ff" stop-opacity="0" />
      <stop offset="0.55" stop-color="#8fd0ff" stop-opacity="0.95" />
      <stop offset="1" stop-color="#4aa0ff" stop-opacity="0.2" />
    </linearGradient>
    <radialGradient id="hero-planet" cx="50%" cy="0%" r="85%">
      <stop offset="0" stop-color="#14396e" />
      <stop offset="0.5" stop-color="#091a35" />
      <stop offset="1" stop-color="#040b18" />
    </radialGradient>
  </defs>
  <rect width="900" height="220" fill="url(#hero-glow)" />
  <circle cx="730" cy="470" r="330" fill="url(#hero-planet)" />
  <path
    d="M420 200C520 130 660 118 790 134C850 142 890 158 910 172"
    stroke="url(#hero-rim)"
    stroke-width="2.4"
    stroke-linecap="round"
    fill="none"
  />
  <path
    d="M440 214C550 150 690 142 850 166"
    stroke="#4aa0ff"
    stroke-opacity="0.35"
    stroke-width="9"
    stroke-linecap="round"
    fill="none"
    filter="blur(5px)"
  />
  <g fill="#fff">
    <circle cx="560" cy="34" r="0.7" />
    <circle cx="640" cy="72" r="0.5" />
    <circle cx="470" cy="96" r="0.5" />
    <circle cx="760" cy="28" r="0.7" />
    <circle cx="840" cy="64" r="0.5" />
    <circle cx="372" cy="46" r="0.5" opacity="0.7" />
    <circle cx="690" cy="110" r="0.7" opacity="0.8" />
    <circle cx="880" cy="22" r="0.5" opacity="0.7" />
  </g>
</svg>

<style>
  .art {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
  }
</style>
