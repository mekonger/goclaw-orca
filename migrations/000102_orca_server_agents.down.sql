ALTER TABLE orca_servers
    DROP COLUMN IF EXISTS agent_health,
    DROP COLUMN IF EXISTS agents;
