import { useState } from "react";
import { createBrowserRouter, Navigate, type RouteObject } from "react-router";
import { RouterProvider } from "react-router/dom";
import { Shell } from "@/components/layout/Shell";
import { HistoryPage } from "@/pages/HistoryPage";
import { MoviesPage } from "@/pages/MoviesPage";
import { SeriesPage } from "@/pages/SeriesPage";
import { SettingsPage } from "@/pages/SettingsPage";
import { TorrentsPage } from "@/pages/TorrentsPage";

// routes are exported so that tests can mount them in a memory router.
export const routes: RouteObject[] = [
  {
    element: <Shell />,
    children: [
      { index: true, element: <Navigate to="/movies" replace /> },
      { path: "movies", element: <MoviesPage /> },
      { path: "series", element: <SeriesPage /> },
      { path: "torrents", element: <TorrentsPage /> },
      { path: "history", element: <HistoryPage /> },
      { path: "settings", element: <SettingsPage /> },
    ],
  },
];

export function App() {
  const [router] = useState(() => createBrowserRouter(routes));
  return <RouterProvider router={router} />;
}
