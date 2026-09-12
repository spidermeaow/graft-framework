-- +graft Up

CREATE TABLE machines (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL UNIQUE CHECK (length(trim(name)) > 0),
    status VARCHAR(20) NOT NULL DEFAULT 'ready' CHECK (status IN ('ready', 'offline')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +graft Down

DROP TABLE machines;
