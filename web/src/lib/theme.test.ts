import { afterEach, describe, expect, it } from "vitest";
import { followSystemTheme } from "./theme";

type Listener = (e: { matches: boolean }) => void;

function fakeMedia(initial: boolean) {
  const listeners = new Set<Listener>();
  const mql = {
    matches: initial,
    addEventListener: (_: string, l: Listener) => listeners.add(l),
    removeEventListener: (_: string, l: Listener) => listeners.delete(l),
  };
  const original = window.matchMedia;
  window.matchMedia = ((query: string) => {
    expect(query).toBe("(prefers-color-scheme: dark)");
    return mql;
  }) as unknown as typeof window.matchMedia;
  return {
    change(matches: boolean) {
      mql.matches = matches;
      listeners.forEach((l) => l({ matches }));
    },
    listeners,
    restore: () => (window.matchMedia = original),
  };
}

let restore = () => {};
afterEach(() => {
  restore();
  document.documentElement.classList.remove("dark");
});

describe("followSystemTheme", () => {
  it("sets the dark class when the system prefers dark", () => {
    const media = fakeMedia(true);
    restore = media.restore;
    followSystemTheme();
    expect(document.documentElement).toHaveClass("dark");
  });

  it("follows a change of the system setting", () => {
    const media = fakeMedia(false);
    restore = media.restore;
    const stop = followSystemTheme();
    expect(document.documentElement).not.toHaveClass("dark");
    media.change(true);
    expect(document.documentElement).toHaveClass("dark");
    media.change(false);
    expect(document.documentElement).not.toHaveClass("dark");
    stop();
    expect(media.listeners.size).toBe(0);
  });
});
