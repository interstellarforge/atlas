CREATE TABLE item_follows (
  item_id INTEGER NOT NULL REFERENCES work_items (id),
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  PRIMARY KEY (item_id, user_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE notifications (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  type TEXT NOT NULL CHECK (type IN ('assigned', 'mention', 'blocker_done', 'followed')),
  project TEXT NOT NULL,
  item_key TEXT NOT NULL,
  actor TEXT NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  read_at INTEGER
) STRICT;

CREATE INDEX notifications_user ON notifications (user_id, read_at, id);

-- A notification.created event is only for its recipient.
ALTER TABLE change_events ADD COLUMN target_user INTEGER;
