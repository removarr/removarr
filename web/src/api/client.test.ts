import { afterEach, describe, expect, it, vi } from "vitest";
import { api, ApiError, rowsBody } from "./client";

function mockFetch(status: number, body?: unknown) {
  const fn = vi.fn().mockResolvedValue(
    new Response(body === undefined ? null : JSON.stringify(body), {
      status,
      headers: body === undefined ? {} : { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fn);
  return fn;
}

// rejection resolves to the error a request rejects with, and fails if it resolves.
function rejection(p: Promise<unknown>): Promise<ApiError> {
  return p.then(
    () => {
      throw new Error("the request did not fail");
    },
    (e: ApiError) => e,
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("api", () => {
  it("sends the request header and JSON on a request that changes something", async () => {
    const fetch = mockFetch(204);
    await api("/api/ignores", { method: "POST", body: { rows: [] } });
    const [, init] = fetch.mock.calls[0];
    expect(init.method).toBe("POST");
    expect(new Headers(init.headers).get("X-Removarr-Request")).toBe("1");
    expect(new Headers(init.headers).get("Content-Type")).toBe("application/json");
    expect(init.body).toBe('{"rows":[]}');
  });

  it("returns undefined for 204 and the parsed body otherwise", async () => {
    mockFetch(204);
    expect(await api("/api/ignores", { method: "DELETE", body: {} })).toBeUndefined();
    mockFetch(200, { status: "ok" });
    expect(await api("/api/health")).toEqual({ status: "ok" });
  });

  it("turns an error body into an ApiError with the server's message and field", async () => {
    mockFetch(400, { error: "must be at least 0", field: "rules.graceDays" });
    const err = await rejection(api("/api/settings", { method: "PUT", body: {} }));
    expect(err).toBeInstanceOf(ApiError);
    expect(err.status).toBe(400);
    expect(err.message).toBe("must be at least 0");
    expect(err.field).toBe("rules.graceDays");
  });

  it("shows the host refusal message of a 403", async () => {
    mockFetch(403, { error: "this server does not answer to the host name in the request; reach it by its IP address, or list the name in REMOVARR_ALLOWED_HOSTS" });
    const err = await rejection(api("/api/status"));
    expect(err.message).toContain("REMOVARR_ALLOWED_HOSTS");
  });

  it("gives a plain message when the body is not JSON", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response("Bad Gateway", { status: 502 })));
    const err = await rejection(api("/api/status"));
    expect(err.message).toBe("The server answered 502.");
  });

  it("names a failed connection", async () => {
    vi.stubGlobal("fetch", vi.fn().mockRejectedValue(new TypeError("Failed to fetch")));
    const err = await rejection(api("/api/status"));
    expect(err.message).toBe("removarr could not be reached.");
    expect(err.status).toBe(0);
  });
});

describe("rowsBody", () => {
  it("sends only list and key, because the server refuses other fields", () => {
    const body = rowsBody([{ list: "series", key: "tvdb:1:s2", title: "x", season: 2 } as never]);
    expect(body).toEqual({ rows: [{ list: "series", key: "tvdb:1:s2" }] });
  });
});
