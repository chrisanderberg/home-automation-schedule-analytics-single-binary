package storage

const Schema = `
CREATE TABLE IF NOT EXISTS controls (
  control_id TEXT PRIMARY KEY,
  control_type TEXT NOT NULL,
  num_states INTEGER NOT NULL,
  state_labels TEXT
);

CREATE TABLE IF NOT EXISTS models (
  control_id TEXT NOT NULL,
  model_id TEXT NOT NULL,
  PRIMARY KEY (control_id, model_id),
  FOREIGN KEY (control_id) REFERENCES controls(control_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS aggregates (
  control_id TEXT NOT NULL,
  model_id TEXT NOT NULL,
  quarter_index INTEGER NOT NULL,
  blob BLOB NOT NULL,
  PRIMARY KEY (control_id, model_id, quarter_index),
  FOREIGN KEY (control_id) REFERENCES controls(control_id) ON DELETE CASCADE
);

`
