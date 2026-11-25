-- The sweep deletes the conversations idle for 180 days; their turns,
-- calls, events, inputs and documents go by the cascades.
CREATE INDEX agent_conversations_idle ON agent_conversations (last_activity_at);
