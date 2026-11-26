import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, expect, vi } from "vitest";
import { takeUnexpectedRequests } from "@/test/render";

// jsdom has no matchMedia. The stub reports a wide screen, so the table layout is the
// default; a test that needs the narrow layout replaces window.matchMedia itself.
Object.defineProperty(window, "matchMedia", {
  configurable: true,
  writable: true,
  value: (query: string): MediaQueryList => ({
    matches: query === "(min-width: 768px)",
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  const missed = takeUnexpectedRequests();
  expect(missed, "requests without a mock route").toEqual([]);
});
