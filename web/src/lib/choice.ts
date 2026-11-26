// A part of the page that a person can open and close, such as the side panel or the scan
// details, is remembered per browser as "open" or "closed" under its own key. Storage can
// throw (blocked site data, a private window): the part then starts open and the choice
// lasts until the page is reloaded.

export function readOpen(key: string): boolean {
  try {
    return localStorage.getItem(key) !== "closed";
  } catch {
    return true;
  }
}

export function writeOpen(key: string, open: boolean) {
  try {
    localStorage.setItem(key, open ? "open" : "closed");
  } catch {
    // Not remembered.
  }
}
