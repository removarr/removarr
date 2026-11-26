import {
  Film, History, Magnet, PanelLeftClose, PanelLeftOpen, Plug, Settings, SlidersHorizontal, Tv, Users,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useId, useRef, useState, type CSSProperties } from "react";
import { NavLink, Outlet } from "react-router";
import { Header } from "@/components/header/Header";
import { Button } from "@/components/ui/button";
import { useMediaQuery } from "@/lib/useMediaQuery";
import { cn } from "@/lib/utils";

interface Item { to: string; label: string; icon: LucideIcon }

const PAGES: Item[] = [
  { to: "/movies", label: "Movies", icon: Film },
  { to: "/series", label: "Series", icon: Tv },
  { to: "/torrents", label: "Torrents", icon: Magnet },
  { to: "/history", label: "History", icon: History },
];
const SETTINGS: Item = { to: "/settings", label: "Settings", icon: Settings };
const SETTINGS_PAGES: Item[] = [
  { to: "/settings/connections", label: "Connections", icon: Plug },
  { to: "/settings/counting-users", label: "Counting users", icon: Users },
  { to: "/settings/rules", label: "Rules", icon: SlidersHorizontal },
];

// The width of the panel on a wide screen. The bulk bar is fixed to the window and reads it
// from --content-left, so that it covers the content and never the panel.
const PANEL_WIDTH = "14rem";
const STORAGE_KEY = "removarr.sidebar";

// Storage can throw (blocked site data, a private window): the panel then starts shown and
// the choice is not remembered.
function readOpen(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) !== "closed";
  } catch {
    return true;
  }
}

function writeOpen(open: boolean) {
  try {
    localStorage.setItem(STORAGE_KEY, open ? "open" : "closed");
  } catch {
    // The choice lasts until the page is reloaded.
  }
}

function MenuLink({ item, current, onChoose, nested }: {
  item: Item; current: "page" | "true"; onChoose: () => void; nested?: boolean;
}) {
  const Icon = item.icon;
  return (
    <NavLink
      to={item.to}
      aria-current={current}
      onClick={onChoose}
      className={({ isActive }) =>
        cn(
          "flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium",
          nested && "py-1.5",
          !isActive && "text-muted-foreground hover:bg-accent/50 hover:text-foreground",
          // The page shown is highlighted; Settings, around its shown sub-page, is in bold.
          isActive && (current === "page" ? "bg-accent text-accent-foreground" : "font-semibold text-foreground"),
        )
      }
    >
      <Icon aria-hidden="true" className="size-4 shrink-0" />
      {item.label}
    </NavLink>
  );
}

// Shell is the frame around every page: the top bar, the side panel and the page itself.
//
// On a wide screen the panel sits beside the content and can be hidden; the choice is kept in
// the browser. On a narrow screen it is a drawer over the content, closed until the toggle
// opens it, and closed again by Escape, the backdrop or a chosen link.
//
// NavLink marks the current page with aria-current="page". Settings is marked with
// aria-current="true" on its sub-pages, which are the current page themselves.
export function Shell() {
  const wide = useMediaQuery("(min-width: 768px)");
  const [wideOpen, setWideOpen] = useState(readOpen);
  const [drawerOpen, setDrawerOpen] = useState(false);
  // A drawer left open must not reappear when the screen turns narrow again.
  const [drawerFor, setDrawerFor] = useState(wide);
  if (drawerFor !== wide) {
    setDrawerFor(wide);
    setDrawerOpen(false);
  }
  const open = wide ? wideOpen : drawerOpen;
  const panelId = useId();
  const toggleRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLElement>(null);

  const toggle = () => {
    if (wide) {
      setWideOpen(!wideOpen);
      writeOpen(!wideOpen);
    } else {
      setDrawerOpen(!drawerOpen);
    }
  };
  const closeDrawer = () => {
    if (!wide) setDrawerOpen(false);
  };

  const drawer = !wide && drawerOpen;
  useEffect(() => {
    if (!drawer) return;
    panelRef.current?.querySelector("a")?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setDrawerOpen(false);
      toggleRef.current?.focus();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [drawer]);

  const shellStyle = { "--content-left": wide && open ? PANEL_WIDTH : "0px" } as CSSProperties;

  return (
    <div className="flex min-h-svh flex-col" style={shellStyle}>
      <header className="border-b">
        <div className="flex flex-wrap items-center gap-4 px-4 py-3">
          <div className="flex items-center gap-2">
            <Button
              ref={toggleRef}
              type="button"
              variant="ghost"
              size="icon"
              aria-label={open ? "Hide menu" : "Show menu"}
              aria-expanded={open}
              aria-controls={panelId}
              onClick={toggle}
            >
              {open ? <PanelLeftClose aria-hidden="true" /> : <PanelLeftOpen aria-hidden="true" />}
            </Button>
            <span className="text-lg font-semibold">removarr</span>
          </div>
          <Header />
        </div>
      </header>
      <div className="flex flex-1">
        {drawer && (
          <div
            data-testid="menu-backdrop"
            aria-hidden="true"
            className="fixed inset-0 z-40 bg-black/50"
            onClick={() => setDrawerOpen(false)}
          />
        )}
        <nav
          ref={panelRef}
          id={panelId}
          aria-label="Main"
          hidden={!open}
          style={wide ? { width: PANEL_WIDTH } : undefined}
          className={cn(
            "shrink-0 border-r bg-background",
            !wide && "fixed inset-y-0 left-0 z-50 w-64 max-w-[80vw] overflow-y-auto shadow-lg",
          )}
        >
          {/* Beside the content the panel runs the full height; its links stay in view on scroll. */}
          <ul
            className={cn(
              "flex flex-col gap-1 p-3",
              wide ? "sticky top-0 max-h-svh overflow-y-auto" : "pb-[max(0.75rem,env(safe-area-inset-bottom))]",
            )}
          >
            {PAGES.map((item) => (
              <li key={item.to}>
                <MenuLink item={item} current="page" onChoose={closeDrawer} />
              </li>
            ))}
            <li>
              <MenuLink item={SETTINGS} current="true" onChoose={closeDrawer} />
              <ul aria-label="Settings pages" className="mt-1 ml-4 flex flex-col gap-1 border-l pl-2">
                {SETTINGS_PAGES.map((item) => (
                  <li key={item.to}>
                    <MenuLink item={item} current="page" onChoose={closeDrawer} nested />
                  </li>
                ))}
              </ul>
            </li>
          </ul>
        </nav>
        <main className="mx-auto w-full min-w-0 max-w-7xl flex-1 px-4 py-6">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
