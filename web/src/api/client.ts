import type { ListKind } from "./types";

export class ApiError extends Error {
  readonly status: number;
  readonly field?: string;

  constructor(message: string, status: number, field?: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.field = field;
  }
}

const CHANGES = new Set(["POST", "PUT", "DELETE", "PATCH"]);

// api calls removarr's HTTP API. A request that changes something carries the header the
// server requires (spec 10). It resolves to undefined for 204 and rejects with an ApiError
// that carries the server's message.
export async function api<T = unknown>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const method = init.method ?? "GET";
  const headers: Record<string, string> = { Accept: "application/json" };
  if (CHANGES.has(method)) {
    headers["X-Removarr-Request"] = "1";
    headers["Content-Type"] = "application/json";
  }
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      headers,
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
    });
  } catch {
    throw new ApiError("removarr could not be reached.", 0);
  }
  if (res.status === 204) return undefined as T;
  let data: unknown;
  try {
    data = await res.json();
  } catch {
    if (!res.ok) throw new ApiError(`The server answered ${res.status}.`, res.status);
    throw new ApiError("The server's answer could not be read.", res.status);
  }
  if (!res.ok) {
    const e = (data ?? {}) as { error?: string; field?: string };
    throw new ApiError(e.error || `The server answered ${res.status}.`, res.status, e.field);
  }
  return data as T;
}

// rowsBody names rows by list and key only: the server refuses any other field.
export function rowsBody(rows: { list: ListKind; key: string }[]) {
  return { rows: rows.map(({ list, key }) => ({ list, key })) };
}
