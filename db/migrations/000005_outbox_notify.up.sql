CREATE FUNCTION outbox_notify() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_notify('cardplay_outbox', json_build_object('room_id', NEW.room_id, 'kind', NEW.kind)::text);
 RETURN NEW;
END $$;
CREATE TRIGGER outbox_notify AFTER INSERT ON outbox FOR EACH ROW EXECUTE FUNCTION outbox_notify();
DROP INDEX outbox_pending_idx;
CREATE INDEX outbox_created_idx ON outbox(created_at);
