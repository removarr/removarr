CREATE TABLE actions (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id     TEXT NOT NULL,
  executed_at TEXT NOT NULL,
  dry_run     INTEGER NOT NULL,
  outcome     TEXT NOT NULL,
  size        INTEGER NOT NULL,
  data        TEXT NOT NULL -- the whole record as JSON
);

-- A plan built before this version lists its steps in the old order, which could delete a
-- file before the episode it holds is unmonitored. Plans expire after ten minutes anyway:
-- dropping them only asks the user to build the plan again.
DELETE FROM plans;
