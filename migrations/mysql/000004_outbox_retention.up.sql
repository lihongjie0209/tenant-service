CREATE INDEX tenant_outbox_retention_idx ON tenant_outbox_events (published_at, id);
