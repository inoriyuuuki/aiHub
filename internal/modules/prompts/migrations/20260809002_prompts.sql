CREATE TABLE IF NOT EXISTS prompt_categories (
    id BIGSERIAL PRIMARY KEY,
    parent_id BIGINT REFERENCES prompt_categories(id),
    project_id BIGINT REFERENCES projects(id),
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    icon TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    archived BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_prompt_categories_global ON prompt_categories(slug) WHERE project_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_prompt_categories_project ON prompt_categories(project_id, slug) WHERE project_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS prompt_schemas (
    id BIGSERIAL PRIMARY KEY,
    category_id BIGINT NOT NULL REFERENCES prompt_categories(id) ON DELETE CASCADE,
    version INT NOT NULL,
    schema JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (category_id, version)
);

CREATE TABLE IF NOT EXISTS prompts (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT REFERENCES projects(id),
    category_id BIGINT NOT NULL REFERENCES prompt_categories(id),
    slug TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    tags TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
    current_version_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_prompts_global ON prompts(slug) WHERE project_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_prompts_project ON prompts(project_id, slug) WHERE project_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_prompts_category ON prompts(category_id);
CREATE INDEX IF NOT EXISTS idx_prompts_status ON prompts(status);

CREATE TABLE IF NOT EXISTS prompt_versions (
    id BIGSERIAL PRIMARY KEY,
    prompt_id BIGINT NOT NULL REFERENCES prompts(id) ON DELETE CASCADE,
    version INT NOT NULL,
    content JSONB NOT NULL,
    variables JSONB NOT NULL DEFAULT '{}',
    schema_id BIGINT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (prompt_id, version)
);

CREATE TABLE IF NOT EXISTS assets (
    id BIGSERIAL PRIMARY KEY,
    object_key TEXT NOT NULL UNIQUE,
    size BIGINT NOT NULL DEFAULT 0,
    sha256 TEXT NOT NULL DEFAULT '',
    mime TEXT NOT NULL DEFAULT '',
    filename TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'file',
    ref_type TEXT NOT NULL DEFAULT '',
    ref_id BIGINT NOT NULL DEFAULT 0,
    ref_version_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_assets_ref ON assets(ref_type, ref_id);
