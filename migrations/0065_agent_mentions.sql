-- The records a member mentioned in a question, [{kind, id, label}]. The
-- index finds the turns that mention one record with @>.
ALTER TABLE agent_turns ADD COLUMN attachments jsonb NOT NULL DEFAULT '[]';

CREATE INDEX agent_turns_attachments ON agent_turns USING gin (attachments jsonb_path_ops);
