-- A run reads its conversation's documents, and deleting a turn sets
-- turn_id NULL in both document tables.
CREATE INDEX agent_documents_conversation ON agent_documents (conversation_id);
CREATE INDEX agent_documents_turn ON agent_documents (turn_id);
CREATE INDEX agent_document_versions_turn ON agent_document_versions (turn_id);

-- Nothing closed or archived a conversation, and nothing read a call's
-- source.
ALTER TABLE agent_conversations DROP COLUMN status;
ALTER TABLE agent_tool_calls DROP COLUMN tool_source;

-- An agent's instructions are its latest version's. The version takes the
-- text the agent ran with where the two differ.
UPDATE ai_agent_versions AS v SET instructions = a.instructions
FROM ai_agents AS a
WHERE v.agent_id = a.id AND v.version_no = a.instructions_version AND v.instructions <> a.instructions;
INSERT INTO ai_agent_versions (agent_id, version_no, instructions, created_by, created_at)
SELECT
    a.id,
    a.instructions_version,
    a.instructions,
    a.updated_by,
    a.updated_at
FROM ai_agents AS a
WHERE NOT EXISTS (
    SELECT 1 FROM ai_agent_versions AS v
    WHERE v.agent_id = a.id AND v.version_no = a.instructions_version
);
ALTER TABLE ai_agents DROP COLUMN instructions;
ALTER TABLE ai_agents ADD CONSTRAINT ai_agents_app_check CHECK (agent_kind = 'console' OR cardinality(tools) = 0);
ALTER TABLE ai_agents ADD CONSTRAINT ai_agents_latest_version FOREIGN KEY (id, instructions_version)
REFERENCES ai_agent_versions (agent_id, version_no) DEFERRABLE INITIALLY DEFERRED;

-- The outputs the 90-day sweep has yet to blank.
CREATE INDEX agent_tool_calls_output_kept ON agent_tool_calls (created_at)
WHERE tool_output IS NOT NULL OR result_content IS NOT NULL;
