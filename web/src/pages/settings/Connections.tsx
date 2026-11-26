import { useEffect, useState } from "react";
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

  const [resets, setResets] = useState<Record<string, number>>({});

  const setEdit = (app: AppName, name: string, value: string | null | undefined) => {
    // An edit equal to the shown value is no change (typing and deleting a character).
    const view = settings.connections[app]?.[name];
    if (typeof value === "string" && view && view.value !== undefined && value === view.value) value = undefined;
    setEdits((e) => ({ ...e, [app]: { ...e[app], [name]: value } }));
    setSaved(false);
  };
  const changes = connectionChanges(edits);

  const onSave = () => {
    if (!changes) return;
    setErrors({});
    setGeneral(undefined);
    const sent = changes;
    save.mutate({ connections: sent }, {
      onSuccess: () => {
        // An old test result of a saved app no longer describes its connection.
        setResets((r) => {
          const next = { ...r };
          for (const app of Object.keys(sent)) next[app] = (next[app] ?? 0) + 1;
          return next;
        });
        setEdits(emptyEdits());
        setSaved(true);
      },
      onError: (e) => {
        if (e instanceof ApiError && e.field) setErrors({ [e.field]: e.message });
        else setGeneral(e.message);
      },
    });
  };

  return (
    <section aria-label="Connections" className="space-y-4">
      <h2 className="text-lg font-semibold">Connections</h2>
      {/* A save that succeeds drops the edits, so nothing can be typed while it is pending. */}
      <fieldset disabled={save.isPending} className="grid min-w-0 gap-4 md:grid-cols-2">
        {APPS.map((a) => (
          <AppCard
            key={a.app} app={a.app} title={a.title} fields={a.fields} views={settings.connections[a.app] ?? {}}
            resetToken={resets[a.app] ?? 0} edits={edits[a.app]} errors={errors} changed={changes?.[a.app] !== undefined}
            onEdit={(name, v) => setEdit(a.app, name, v)}
          />
        ))}
      </fieldset>
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
  resetToken: number; onEdit: (name: string, value: string | null | undefined) => void;
}

function AppCard({ app, title, fields, views, edits, errors, changed, resetToken, onEdit }: AppCardProps) {
  const test = useTestConnection(app);
  const { reset } = test;
  useEffect(() => { if (resetToken > 0) reset(); }, [resetToken, reset]);
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
                // Browsers ignore "off" on a password field and fill saved logins, which would
                // become edits the user never typed. "new-password" asks them not to; the name
                // is not a login pattern.
                autoComplete={f.secret ? "new-password" : "off"}
                name={`removarr-${app}-${f.name}`}
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
              {!f.secret && edit === "" && view.value && (
                <p className="text-sm text-muted-foreground">
                  Empty fields are not saved. Use Clear saved value to remove a saved value.
                </p>
              )}
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
