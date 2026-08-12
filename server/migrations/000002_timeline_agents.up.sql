CREATE TABLE timeline_events (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    delivery_id uuid NOT NULL REFERENCES deliveries(id) ON DELETE CASCADE,
    stage       text NOT NULL,
    event_type  text NOT NULL,
    payload     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX timeline_events_delivery_idx ON timeline_events(delivery_id, created_at);

CREATE TYPE agent_role AS ENUM ('spec', 'test', 'coder', 'reviewer');

CREATE TABLE agent_configs (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name       text NOT NULL UNIQUE,
    role       agent_role NOT NULL,
    config     jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
