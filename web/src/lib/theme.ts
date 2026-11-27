// followSystemTheme sets the dark class on the root element while the system prefers a dark
// colour scheme, and keeps it in step when that setting changes (spec 7.7). It returns a
// function that stops following.
export function followSystemTheme(): () => void {
  const media = window.matchMedia("(prefers-color-scheme: dark)");
  const apply = (dark: boolean) => document.documentElement.classList.toggle("dark", dark);
  apply(media.matches);
  const onChange = (e: { matches: boolean }) => apply(e.matches);
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}
