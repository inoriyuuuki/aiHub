CREATE TABLE IF NOT EXISTS expert_packs (
    id BIGSERIAL PRIMARY KEY,
    project_id BIGINT REFERENCES projects(id),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    domain TEXT NOT NULL DEFAULT '',
    responsibility TEXT NOT NULL DEFAULT '',
    usage TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','published','archived')),
    current_version_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS expert_pack_versions (
    id BIGSERIAL PRIMARY KEY,
    pack_id BIGINT NOT NULL REFERENCES expert_packs(id) ON DELETE CASCADE,
    version INT NOT NULL,
    manifest JSONB NOT NULL,
    sha256 TEXT NOT NULL,
    object_key TEXT NOT NULL,
    size BIGINT NOT NULL,
    changelog TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (pack_id, version)
);

CREATE TABLE IF NOT EXISTS expert_members (
    id BIGSERIAL PRIMARY KEY,
    pack_id BIGINT NOT NULL REFERENCES expert_packs(id) ON DELETE CASCADE,
    skill_id BIGINT NOT NULL REFERENCES skills(id),
    skill_version_id BIGINT NOT NULL REFERENCES skill_versions(id),
    install_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (pack_id, skill_id)
);
