-- Full-text index over item title and description (ATL-76). External content: the text stays in work_items.
CREATE VIRTUAL TABLE items_fts USING fts5 (title, description, content = 'work_items', content_rowid = 'id', tokenize = 'unicode61 remove_diacritics 2');

INSERT INTO items_fts (items_fts) VALUES ('rebuild');

CREATE TRIGGER items_fts_ai AFTER INSERT ON work_items BEGIN
  INSERT INTO items_fts (rowid, title, description) VALUES (new.id, new.title, new.description);
END;

CREATE TRIGGER items_fts_ad AFTER DELETE ON work_items BEGIN
  INSERT INTO items_fts (items_fts, rowid, title, description) VALUES ('delete', old.id, old.title, old.description);
END;

CREATE TRIGGER items_fts_au AFTER UPDATE OF title, description ON work_items BEGIN
  INSERT INTO items_fts (items_fts, rowid, title, description) VALUES ('delete', old.id, old.title, old.description);
  INSERT INTO items_fts (rowid, title, description) VALUES (new.id, new.title, new.description);
END;
