CREATE TABLE IF NOT EXISTS mcp_definitions (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT REFERENCES projects(id),
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    tags TEXT[] NOT NULL DEFAULT '{}',
    transport TEXT NOT NULL DEFAULT 'stdio' CHECK (transport IN ('stdio','http')),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
    current_version_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_mcp_defs_global ON mcp_definitions(slug) WHERE project_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_mcp_defs_project ON mcp_definitions(project_id, slug) WHERE project_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS mcp_definition_versions (
    id BIGSERIAL PRIMARY KEY,
    definition_id BIGINT NOT NULL REFERENCES mcp_definitions(id) ON DELETE CASCADE,
    version INT NOT NULL,
    config JSONB NOT NULL,
    env_vars JSONB NOT NULL DEFAULT '[]',
    tools JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (definition_id, version)
);

CREATE TABLE IF NOT EXISTS mcp_profiles (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    scope TEXT NOT NULL DEFAULT 'global' CHECK (scope IN ('global','project')),
    project_id BIGINT REFERENCES projects(id),
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS mcp_profile_items (
    id BIGSERIAL PRIMARY KEY,
    profile_id BIGINT NOT NULL REFERENCES mcp_profiles(id) ON DELETE CASCADE,
    definition_id BIGINT NOT NULL REFERENCES mcp_definitions(id),
    definition_version_id BIGINT NOT NULL REFERENCES mcp_definition_versions(id),
    enabled_tools TEXT[] NOT NULL DEFAULT '{}',
    disabled_tools TEXT[] NOT NULL DEFAULT '{}',
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_mcp_profile_items ON mcp_profile_items(profile_id);
