// Sets the theme the toggle last chose here, before the page paints, so it
// never flashes the other one. Nothing chosen: the system's.
try {
  const t = localStorage.getItem("theme");
  if (t === "dark" || t === "light") document.documentElement.dataset.theme = t;
} catch {}
