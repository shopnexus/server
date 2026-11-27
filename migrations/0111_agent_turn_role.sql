-- A turn keeps the member's console role from the start of its run, so an
-- admin's operation (x-role: admin) is left out of a moderator's turn.
-- Earlier turns count as a moderator's.
ALTER TABLE agent_turns
ADD COLUMN member_role text NOT NULL DEFAULT 'moderator' CHECK (member_role IN ('moderator', 'admin'));
ALTER TABLE agent_turns ALTER COLUMN member_role DROP DEFAULT;
