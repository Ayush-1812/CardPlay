DROP INDEX sessions_id_idx;
ALTER TABLE sessions DROP COLUMN id;
ALTER TABLE users DROP COLUMN deleted_at;
