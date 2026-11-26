import { NavLink, Outlet } from "react-router";
import { cn } from "@/lib/utils";

const LINKS = [
  { to: "/movies", label: "Movies" },
  { to: "/series", label: "Series" },
  { to: "/torrents", label: "Torrents" },
  { to: "/history", label: "History" },
  { to: "/settings", label: "Settings" },
];

// Shell is the frame around every page: the top bar, the navigation and the page itself.
// NavLink marks the current page with aria-current="page".
export function Shell() {
  return (
    <div className="flex min-h-svh flex-col">
      <header className="border-b">
        <div className="mx-auto flex max-w-7xl flex-wrap items-center gap-4 px-4 py-3">
          <span className="text-lg font-semibold">removarr</span>
          {/* The scan button, the summary and the free space go here (task 4). */}
          <div className="flex flex-1 flex-wrap items-center justify-end gap-4" />
        </div>
        <nav aria-label="Main" className="mx-auto flex max-w-7xl gap-1 overflow-x-auto px-4">
          {LINKS.map(({ to, label }) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                cn(
                  "border-b-2 px-3 py-2 text-sm font-medium whitespace-nowrap",
                  isActive ? "border-primary text-foreground" : "border-transparent text-muted-foreground hover:text-foreground",
                )
              }
            >
              {label}
            </NavLink>
          ))}
        </nav>
      </header>
      <main className="mx-auto w-full max-w-7xl flex-1 px-4 py-6">
        <Outlet />
      </main>
    </div>
  );
}
