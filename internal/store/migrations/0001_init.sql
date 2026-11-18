CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE scans (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at  TEXT NOT NULL,
  finished_at TEXT,
  status      TEXT NOT NULL,
  summary     TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE candidates (
  scan_id INTEGER NOT NULL REFERENCES scans(id) ON DELETE CASCADE,
  list    TEXT NOT NULL,
  key     TEXT NOT NULL,
  data    TEXT NOT NULL,
  PRIMARY KEY (scan_id, list, key)
);
