import { QueryClientProvider } from "@tanstack/react-query";
import { makeQueryClient } from "@/api/queryClient";
import { render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { beforeEach, describe, expect, it } from "vitest";
import { TooltipProvider } from "@/components/ui/tooltip";
import { mockApi } from "@/test/render";
import { routes } from "./App";

// The shell's header reads the status; a list page reads its list.
beforeEach(() => {
  const list = (name: string) => () => ({ status: 200, body: { list: name, scannedAt: null, candidates: null } });
  mockApi({
    "GET /api/actions?limit=50": () => ({ status: 200, body: { actions: [] } }),
    "GET /api/candidates?list=movies&ignored=false": list("movies"),
    "GET /api/candidates?list=series&ignored=false": list("series"),
    "GET /api/candidates?list=torrents&ignored=false": list("torrents"),
    "GET /api/settings": () => ({ status: 500, body: { error: "not needed here" } }),
    "GET /api/media-server/users": () => ({ status: 500, body: { error: "not needed here" } }),
    "GET /api/status": () => ({
      status: 200,
      body: { dryRun: false, connections: {}, scan: { running: false }, lastScan: null, stale: false },
    }),
  });
});

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const queryClient = makeQueryClient();
  queryClient.setDefaultOptions({ ...queryClient.getDefaultOptions(), queries: { retry: false } });
  render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
      </TooltipProvider>
    </QueryClientProvider>,
  );
  return router;
}

describe("the app shell", () => {
  it("sends the root to the movie list", async () => {
    const router = renderAt("/");
    expect(await screen.findByRole("heading", { level: 1, name: "Movies" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/movies");
  });

  it.each([
    ["/movies", "Movies"],
    ["/series", "Series"],
    ["/torrents", "Torrents"],
    ["/history", "History"],
    ["/settings/connections", "Connections"],
    ["/settings/counting-users", "Counting users"],
    ["/settings/rules", "Rules"],
  ])("shows the page of %s and marks its link as the current one", async (path, name) => {
    renderAt(path);
    expect(await screen.findByRole("heading", { level: 1, name })).toBeInTheDocument();
    const nav = screen.getByRole("navigation");
    const current = nav.querySelectorAll('[aria-current="page"]');
    expect(current).toHaveLength(1);
    expect(current[0]).toHaveTextContent(name);
  });

  it("names the app in the top bar", () => {
    renderAt("/movies");
    expect(screen.getByText("removarr")).toBeInTheDocument();
  });

  it("shows the logo beside the name in the top bar", () => {
    renderAt("/movies");
    const logo = screen.getByTestId("app-logo");
    expect(logo).toHaveAttribute("src", "/logo.svg");
    // The name is next to it, so the image is decorative.
    expect(logo).toHaveAttribute("alt", "");
    expect(logo.parentElement).toHaveTextContent("removarr");
  });
});
