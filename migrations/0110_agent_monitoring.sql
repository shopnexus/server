-- The Console assistant reads monitoring: the system's health, metrics and
-- logs (UC-MOD-13). The server answers only an admin's turn.
UPDATE ai_agents SET
    capabilities = array(
        SELECT DISTINCT c FROM unnest(capabilities || 'monitoring.read'::text) AS t (c)
        ORDER BY c
    )
WHERE id = '0193e000-0000-7000-8000-00000000a101';
