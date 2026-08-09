CREATE TABLE IF NOT EXISTS skills (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT REFERENCES projects(id),
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    tags TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
    current_version_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_skills_global ON skills(slug) WHERE project_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_skills_project ON skills(project_id, slug) WHERE project_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS skill_versions (
    id BIGSERIAL PRIMARY KEY,
    skill_id BIGINT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    version INT NOT NULL,
    object_key TEXT NOT NULL,
    sha256 TEXT NOT NULL,
    size BIGINT NOT NULL,
    root_dir TEXT NOT NULL DEFAULT '',
    files JSONB NOT NULL DEFAULT '[]',
    summary TEXT NOT NULL DEFAULT '',
    changelog TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (skill_id, version)
);
