import type { ReactNode } from "react";
import { useSettings } from "@/api/queries";
import type { Settings } from "@/api/types";
import { Connections } from "./settings/Connections";
import { CountingUsers } from "./settings/CountingUsers";
import { Rules } from "./settings/Rules";

// SettingsSubPage is one page of the settings: its heading, the error of reading the
// settings, and the section once they are read.
function SettingsSubPage({ title, children }: { title: string; children: (settings: Settings) => ReactNode }) {
  const settings = useSettings();
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">{title}</h1>
      {settings.error && <p role="alert" className="text-destructive">{settings.error.message}</p>}
      {settings.data && children(settings.data)}
    </div>
  );
}

export function ConnectionsPage() {
  return (
    <SettingsSubPage title="Connections">
      {(s) => (
        <>
          {s.dryRun && <p className="text-sm">Dry run is on: set by REMOVARR_DRY_RUN.</p>}
          <Connections settings={s} />
        </>
      )}
    </SettingsSubPage>
  );
}

export function CountingUsersPage() {
  return <SettingsSubPage title="Counting users">{(s) => <CountingUsers settings={s} />}</SettingsSubPage>;
}

export function RulesPage() {
  return <SettingsSubPage title="Rules">{(s) => <Rules settings={s} />}</SettingsSubPage>;
}
