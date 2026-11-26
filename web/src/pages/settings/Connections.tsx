import { useState } from "react";
import { ApiError } from "@/api/client";
import { useSaveSettings, useTestConnection } from "@/api/queries";
import type { AppName, FieldView, Settings, Source } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { connectionChanges, type ConnectionEdits } from "./changes";

interface FieldDef { name: string; label: string; secret?: boolean }
const APPS: { app: AppName; title: string; fields: FieldDef[] }[] = [
  { app: "jellyfin", title: "Jellyfin", fields: [{ name: "url", label: "URL" }, { name: "apiKey", label: "API key", secret: true }] },
  { app: "sonarr", title: "Sonarr", fields: [{ name: "url", label: "URL" }, { name: "apiKey", label: "API key", secret: true }] },
  { app: "radarr", title: "Radarr", fields: [{ name: "url", label: "URL" }, { name: "apiKey", label: "API key", secret: true }] },
  {
    app: "qbittorrent", title: "qBittorrent",
    fields: [{ name: "url", label: "URL" }, { name: "username", label: "User name" }, { name: "password", label: "Password", secret: true }],
  },
];

const SOURCE_TEXT: Record<Source, string> = { ui: "Saved here", env: "From the environment", none: "Not set" };

const emptyEdits = (): ConnectionEdits => ({ jellyfin: {}, sonarr: {}, radarr: {}, qbittorrent: {} });

// Connections edits the four app connections. Only a field the user changed is sent (spec
// 10): the server clears a saved value for null or "".
export function Connections({ settings }: { settings: Settings }) {
  const [edits, setEdits] = useState<ConnectionEdits>(emptyEdits);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [general, setGeneral] = useState<string>();
  const [saved, setSaved] = useState(false);
  const save = useSaveSettings();

  const setEdit = (app: AppName, name: string, value: string | null | undefined) => {
    setEdits((e) => ({ ...e, [app]: { ...e[app], [name]: value } }));
    setSaved(false);
  };
  const changes = connectionChanges(edits);

  const onSave = () => {
    if (!changes) return;
    setErrors({});
    setGeneral(undefined);
    save.mutate({ connections: changes }, {
      onSuccess: () => { setEdits(emptyEdits()); setSaved(true); },
      onError: (e) => {
        if (e instanceof ApiError && e.field) setErrors({ [e.field]: e.message });
        else setGeneral(e.message);
      },
    });
  };

  return (
    <section aria-label="Connections" className="space-y-4">
      <h2 className="text-lg font-semibold">Connections</h2>
      <div className="grid gap-4 md:grid-cols-2">
        {APPS.map((a) => (
          <AppCard
            key={a.app} app={a.app} title={a.title} fields={a.fields} views={settings.connections[a.app] ?? {}}
            edits={edits[a.app]} errors={errors} changed={changes?.[a.app] !== undefined}
            onEdit={(name, v) => setEdit(a.app, name, v)}
          />
        ))}
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <Button onClick={onSave} disabled={!changes || save.isPending}>Save connections</Button>
        {saved && <p role="status" className="text-sm">Saved. Scan again to apply the changes to the lists.</p>}
        {general && <p role="alert" className="text-sm text-destructive">{general}</p>}
      </div>
    </section>
  );
}

interface AppCardProps {
  app: AppName; title: string; fields: FieldDef[]; views: Record<string, FieldView>;
  edits: Record<string, string | null | undefined>; errors: Record<string, string>; changed: boolean;
  onEdit: (name: string, value: string | null | undefined) => void;
}

function AppCard({ app, title, fields, views, edits, errors, changed, onEdit }: AppCardProps) {
  const test = useTestConnection(app);
  return (
    <Card role="group" aria-label={title}>
      <CardHeader><CardTitle>{title}</CardTitle></CardHeader>
      <CardContent className="space-y-4">
        {fields.map((f) => {
          const view = views[f.name] ?? { set: false, source: "none" as Source };
          const id = `${app}-${f.name}`;
          const edit = edits[f.name];
          const err = errors[`connections.${app}.${f.name}`];
          return (
            <div key={f.name} data-field={`connections.${app}.${f.name}`} className="space-y-1.5">
              <div className="flex flex-wrap items-center gap-2">
                <Label htmlFor={id}>{f.label}</Label>
                {f.secret && view.set && <span className="text-sm">Set</span>}
                {f.secret && !view.set ? <Badge variant="outline">Not set</Badge> : <Badge variant="secondary">{SOURCE_TEXT[view.source]}</Badge>}
              </div>
              <Input
                id={id}
                type={f.secret ? "password" : "text"}
                autoComplete="off"
                placeholder={f.secret ? "Enter a new value to replace it" : undefined}
                value={edit === undefined ? (f.secret ? "" : (view.value ?? "")) : (edit ?? "")}
                onChange={(e) => onEdit(f.name, f.secret && e.target.value === "" ? undefined : e.target.value)}
                aria-invalid={err ? true : undefined}
              />
              {edit === null ? (
                <p className="text-sm text-muted-foreground">
                  The saved value is cleared when you save.{" "}
                  <Button type="button" variant="link" className="h-auto p-0" onClick={() => onEdit(f.name, undefined)}>
                    Undo
                  </Button>
                </p>
              ) : view.source === "ui" ? (
                <Button
                  type="button" variant="outline" size="sm" onClick={() => onEdit(f.name, null)}
                  aria-label={`Clear saved value for ${f.label}`}
                >
                  Clear saved value
                </Button>
              ) : null}
              {err && <p role="alert" className="text-sm text-destructive">{err}</p>}
            </div>
          );
        })}
        <div className="flex flex-wrap items-center gap-3">
          <Button type="button" variant="outline" onClick={() => test.mutate()} disabled={changed || test.isPending}>
            Test
          </Button>
          {changed && <span className="text-sm text-muted-foreground">Save to test</span>}
          {test.data?.ok && <p role="status" className="text-sm">Connected: {test.data.detail}</p>}
          {test.data && !test.data.ok && <p role="alert" className="text-sm text-destructive">{test.data.error}</p>}
          {test.error && <p role="alert" className="text-sm text-destructive">{test.error.message}</p>}
        </div>
      </CardContent>
    </Card>
  );
}
