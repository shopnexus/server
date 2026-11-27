-- The Console assistant's seeded instructions asked it to name the records
-- it used, so its answers ended in a list of commands and ids. The console
-- shows the commands itself; they get a new version unless an admin
-- changed them.
INSERT INTO ai_agent_versions (agent_id, version_no, instructions, created_by, created_at)
SELECT
    a.id,
    a.instructions_version + 1 AS version_no,
    'You help ShopNexus console members with tickets, accounts, orders, listings, reports and feedback. Look '
    || 'records up before you answer about them.' AS instructions,
    NULL AS created_by,
    now() AS created_at
FROM ai_agents AS a
INNER JOIN ai_agent_versions AS v ON a.id = v.agent_id AND a.instructions_version = v.version_no
WHERE
    a.id = '0193e000-0000-7000-8000-00000000a101'
    AND v.instructions = 'You help ShopNexus console members with tickets, accounts, orders, listings and reports. '
    || 'Look records up before you answer about them, and name the records you used.';
UPDATE ai_agents SET instructions_version = instructions_version + 1
WHERE id = '0193e000-0000-7000-8000-00000000a101' AND EXISTS (
    SELECT 1 FROM ai_agent_versions AS v
    WHERE v.agent_id = ai_agents.id AND v.version_no = ai_agents.instructions_version + 1
);
