import { describe, expect, it } from "vitest";
import type { AppName } from "@/api/types";
import { connectionChanges } from "./changes";

const none = (): Record<AppName, Record<string, string | null | undefined>> => ({
  jellyfin: {}, sonarr: {}, radarr: {}, qbittorrent: {},
});

describe("connectionChanges", () => {
  it("leaves out untouched fields", () => {
    const e = none();
    e.sonarr = { url: "http://192.0.2.1:8989", apiKey: undefined };
    expect(connectionChanges(e)).toEqual({ sonarr: { url: "http://192.0.2.1:8989" } });
  });

  it("leaves out an emptied secret input", () => {
    const e = none();
    e.radarr = { apiKey: "" };
    expect(connectionChanges(e)).toBeUndefined();
  });

  it("keeps null as a clear", () => {
    const e = none();
    e.radarr = { apiKey: null };
    expect(connectionChanges(e)).toEqual({ radarr: { apiKey: null } });
  });

  it("gives undefined when there are no edits", () => {
    expect(connectionChanges(none())).toBeUndefined();
  });
});
