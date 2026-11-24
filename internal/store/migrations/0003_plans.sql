CREATE TABLE plans (
  id         TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  expires_at INTEGER NOT NULL, -- Unix time in nanoseconds, so that expiry compares as a number
  data       TEXT NOT NULL
);
