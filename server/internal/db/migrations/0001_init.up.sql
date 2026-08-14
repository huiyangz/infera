CREATE TABLE projects (
    id            UUID PRIMARY KEY,
    name          TEXT NOT NULL,
    repo_url      TEXT NOT NULL DEFAULT '',
    default_branch TEXT NOT NULL DEFAULT 'main',
    pinned        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE deliveries (
    id            UUID PRIMARY KEY,
    project_id    UUID NOT NULL REFERENCES projects(id),
    title         TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT 'active',
    current_stage TEXT NOT NULL DEFAULT 'intake',
    pending_gate  TEXT NOT NULL DEFAULT '',
    fail_count    INT NOT NULL DEFAULT 0,
    base_commit   TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_deliveries_project ON deliveries(project_id);

CREATE TABLE stage_runs (
    id          UUID PRIMARY KEY,
    delivery_id UUID NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    stage       TEXT NOT NULL,
    attempt     INT NOT NULL DEFAULT 1,
    status      TEXT NOT NULL DEFAULT 'running',
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ
);

CREATE TABLE artifacts (
    id          UUID PRIMARY KEY,
    delivery_id UUID NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    stage       TEXT NOT NULL,
    kind        TEXT NOT NULL,
    content     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_artifacts_delivery ON artifacts(delivery_id);

CREATE TABLE events (
    id          UUID PRIMARY KEY,
    delivery_id UUID NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    stage       TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_events_delivery ON events(delivery_id);
