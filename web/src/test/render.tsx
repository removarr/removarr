import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { vi } from "vitest";
import { TooltipProvider } from "@/components/ui/tooltip";

// renderWithProviders mounts the UI in a fresh query client (no retries, so a failure shows
// at once) and a memory router. The client is returned so that a test can refetch queries.
export function renderWithProviders(ui: ReactElement, opts: { route?: string } = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const router = createMemoryRouter([{ path: "*", element: ui }], { initialEntries: [opts.route ?? "/"] });
  const result = render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return { ...result, queryClient, router };
}

export type MockResponse = { status: number; body?: unknown };
export type MockRoutes = Record<string, (init: RequestInit) => MockResponse>;

let unexpected: string[] = [];

// takeUnexpectedRequests returns the requests no route answered and forgets them. The setup
// file calls it after every test and fails the test when it is not empty, because the UI
// turns a failed fetch into an error message and a test would not notice otherwise.
export function takeUnexpectedRequests(): string[] {
  const list = unexpected;
  unexpected = [];
  return list;
}

// mockApi stubs fetch. A route is "METHOD path", with the query string when there is one.
// A request without a route is recorded and fails the test.
export function mockApi(routes: MockRoutes) {
  const calls: string[] = [];
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init: RequestInit = {}) => {
    const path = typeof input === "string" ? input : input instanceof URL ? input.pathname + input.search : input.url;
    const key = `${(init.method ?? "GET").toUpperCase()} ${path}`;
    calls.push(key);
    const route = routes[key];
    if (!route) {
      unexpected.push(key);
      throw new TypeError(`no mock route for ${key}`);
    }
    const { status, body } = route(init);
    if (status === 204) return new Response(null, { status });
    return new Response(JSON.stringify(body ?? null), { status, headers: { "Content-Type": "application/json" } });
  });
  vi.stubGlobal("fetch", fetchMock);
  return { calls, fetchMock, count: (key: string) => calls.filter((c) => c === key).length };
}
