DROP INDEX outbox_created_idx;
CREATE INDEX outbox_pending_idx ON outbox(id) WHERE delivered_at IS NULL;
DROP TRIGGER outbox_notify ON outbox;
DROP FUNCTION outbox_notify();
