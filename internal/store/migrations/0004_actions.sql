CREATE TABLE actions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id     TEXT NOT NULL,
  executed_at TEXT NOT NULL,
  dry_run     INTEGER NOT NULL,
  outcome     TEXT NOT NULL,
  size        INTEGER NOT NULL,
  data        TEXT NOT NULL -- the whole record as JSON
);
