-- Which status categories share one board column (comma separated). Replaces the all-or-nothing
-- merge_columns switch; projects that had it on merge every category, as before.
ALTER TABLE projects ADD COLUMN merge_categories TEXT NOT NULL DEFAULT '';
UPDATE projects SET merge_categories = 'todo,active,review,completed' WHERE merge_columns = 1;
