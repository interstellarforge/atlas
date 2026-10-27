// Runs before first paint so the saved theme applies without a flash; a file, because the CSP forbids inline scripts.
try {
  const t = localStorage.getItem("atlas-theme");
  if (t) document.documentElement.dataset.theme = t;
} catch {
  /* storage unavailable */
}
