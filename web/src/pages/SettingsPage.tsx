import { useSettings } from "@/api/queries";
import { Separator } from "@/components/ui/separator";
import { Connections } from "./settings/Connections";
import { CountingUsers } from "./settings/CountingUsers";
import { Rules } from "./settings/Rules";

export function SettingsPage() {
  const settings = useSettings();
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-semibold">Settings</h1>
      {settings.error && <p role="alert" className="text-destructive">{settings.error.message}</p>}
      {settings.data && (
        <>
          {settings.data.dryRun && <p className="text-sm">Dry run is on: set by REMOVARR_DRY_RUN.</p>}
          <Connections settings={settings.data} />
          <Separator />
          <CountingUsers settings={settings.data} />
          <Separator />
          <Rules settings={settings.data} />
        </>
      )}
    </div>
  );
}
