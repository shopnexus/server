-- The console's Idempotency-Key of a question, a steer, a skill and a
-- conversation without a subject, so a retry answers the first row; NULL
-- for rows from before and requests without one.
ALTER TABLE agent_conversations ADD COLUMN request_key uuid;
CREATE UNIQUE INDEX agent_conversations_member_request_key ON agent_conversations (member_id, request_key)
WHERE request_key IS NOT NULL;

ALTER TABLE agent_turns ADD COLUMN request_key uuid;
CREATE UNIQUE INDEX agent_turns_member_request_key ON agent_turns (member_id, request_key)
WHERE request_key IS NOT NULL;

-- An input's member is who sent it, and resumed says a steer continued a
-- paused run, so its retry answers resumed as the first did.
ALTER TABLE agent_turn_inputs ADD COLUMN member_id uuid REFERENCES console_members (id);
ALTER TABLE agent_turn_inputs ADD COLUMN request_key uuid;
ALTER TABLE agent_turn_inputs ADD COLUMN resumed boolean NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX agent_turn_inputs_member_request_key ON agent_turn_inputs (member_id, request_key)
WHERE request_key IS NOT NULL;

-- A skill's change checks the write it read.
ALTER TABLE agent_skills ADD COLUMN request_key uuid;
ALTER TABLE agent_skills ADD COLUMN write_id uuid;
UPDATE agent_skills SET write_id = id;
ALTER TABLE agent_skills ALTER COLUMN write_id SET NOT NULL;
CREATE UNIQUE INDEX agent_skills_member_request_key ON agent_skills (created_by, request_key)
WHERE request_key IS NOT NULL;

-- The budget settings' change checks the write it read.
ALTER TABLE ai_budget_settings ADD COLUMN write_id uuid;
UPDATE ai_budget_settings SET write_id = '0193e000-0000-7000-8000-000000000094';
ALTER TABLE ai_budget_settings ALTER COLUMN write_id SET NOT NULL;
