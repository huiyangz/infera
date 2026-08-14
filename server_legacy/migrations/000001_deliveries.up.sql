CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TYPE delivery_status AS ENUM ('active', 'completed', 'blocked');

CREATE TABLE deliveries (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title         text NOT NULL,
    description   text NOT NULL DEFAULT '',
    repo_url      text NOT NULL DEFAULT '',
    branch        text NOT NULL DEFAULT '',
    status        delivery_status NOT NULL DEFAULT 'active',
    current_stage text NOT NULL DEFAULT 'intake',
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX deliveries_status_idx ON deliveries(status);
