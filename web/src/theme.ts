// Dark (default) or light "paper" theme. Per-device preference.
export type Theme = "dark" | "light";
const KEY = "hub.theme";
const subs = new Set<(t: Theme) => void>();

export function getTheme(): Theme {
  try {
    return localStorage.getItem(KEY) === "light" ? "light" : "dark";
  } catch {
    return "dark";
  }
}

export function applyTheme(t: Theme) {
  document.documentElement.dataset.theme = t;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", t === "light" ? "#f6f1e7" : "#161412");
}

export function setTheme(t: Theme) {
  try {
    localStorage.setItem(KEY, t);
  } catch {
    /* storage unavailable */
  }
  applyTheme(t);
  subs.forEach((f) => f(t));
}

export function onTheme(f: (t: Theme) => void) {
  subs.add(f);
  return () => void subs.delete(f);
}
