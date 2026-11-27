import { QueryClientProvider } from "@tanstack/react-query";
import { makeQueryClient } from "@/api/queryClient";
import { render } from "@testing-library/react";
import type { ReactElement } from "react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { vi } from "vitest";
import { routes } from "@/App";
import { TooltipProvider } from "@/components/ui/tooltip";

// renderWithProviders mounts the UI in a fresh query client (no retries, so a failure shows
// at once) and a memory router. The client is returned so that a test can refetch queries.
export function renderWithProviders(ui: ReactElement, opts: { route?: string } = {}) {
  // The app's own client, so a test sees the app's defaults (a change is never retried).
  // Only query retries are turned off, so that a failure shows at once.
  const queryClient = makeQueryClient();
  queryClient.setDefaultOptions({ ...queryClient.getDefaultOptions(), queries: { retry: false } });
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

// renderApp mounts the app's own routes at a path, with the shell around the page, so a test
// sees the navigation and the redirects as the browser does.
export function renderApp(path: string) {
  const queryClient = makeQueryClient();
  queryClient.setDefaultOptions({ ...queryClient.getDefaultOptions(), queries: { retry: false } });
  const router = createMemoryRouter(routes, { initialEntries: [path] });
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
// A route may answer later: a test that must see a state while a request is pending returns a
// promise and resolves it when it is done looking.
export type MockRoutes = Record<string, (init: RequestInit) => MockResponse | Promise<MockResponse>>;

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
    const { status, body } = await route(init);
    if (status === 204) return new Response(null, { status });
    return new Response(JSON.stringify(body ?? null), { status, headers: { "Content-Type": "application/json" } });
  });
  vi.stubGlobal("fetch", fetchMock);
  return { calls, fetchMock, count: (key: string) => calls.filter((c) => c === key).length };
}
