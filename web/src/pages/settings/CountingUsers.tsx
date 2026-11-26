import { useState } from "react";
import { ApiError } from "@/api/client";
import { useSaveSettings, useUsers } from "@/api/queries";
import type { Settings } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";

// CountingUsers picks the media server users whose watching counts (spec 6.1). The selection
// is saved as rules.countingUsers.
export function CountingUsers({ settings }: { settings: Settings }) {
  const jf = settings.connections.jellyfin;
  const connected = !!jf?.url?.set && !!jf?.apiKey?.set;
  const users = useUsers(connected);
  const save = useSaveSettings();
  const [draft, setDraft] = useState<string[]>();
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string>();

  const selected = draft ?? settings.rules.countingUsers;
  const toggle = (id: string, on: boolean) => {
    setDraft(on ? [...selected, id] : selected.filter((x) => x !== id));
    setSaved(false);
  };
  const onSave = () => {
    if (!draft) return;
    setError(undefined);
    save.mutate({ rules: { countingUsers: draft } }, {
      onSuccess: () => { setDraft(undefined); setSaved(true); },
      onError: (e) => setError(e.message),
    });
  };

  let body;
  if (!connected || (users.error instanceof ApiError && users.error.status === 409)) {
    body = <p className="text-sm text-muted-foreground">Connect Jellyfin first.</p>;
  } else if (users.error) {
    body = <p role="alert" className="text-sm text-destructive">{users.error.message}</p>;
  } else if (users.data) {
    body = (
      <ul className="space-y-2">
        {users.data.map((u) => (
          <li key={u.id} className="flex items-center gap-2">
            <Checkbox id={`user-${u.id}`} checked={selected.includes(u.id)} onCheckedChange={(c) => toggle(u.id, c === true)} />
            <Label htmlFor={`user-${u.id}`}>{u.name}</Label>
          </li>
        ))}
      </ul>
    );
  } else {
    body = <p className="text-sm text-muted-foreground">Loading users...</p>;
  }

  return (
    <section aria-label="Counting users" className="space-y-4">
      <h2 className="text-lg font-semibold">Counting users</h2>
      {body}
      {selected.length === 0 && (
        <p className="text-sm text-muted-foreground">Watched lists are not produced until at least one user is selected.</p>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <Button onClick={onSave} disabled={!draft || save.isPending}>Save counting users</Button>
        {saved && <p role="status" className="text-sm">Saved. Scan again to apply the changes to the lists.</p>}
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
      </div>
    </section>
  );
}
