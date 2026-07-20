CREATE TABLE IF NOT EXISTS position_provenance (
    position_id UUID PRIMARY KEY REFERENCES positions(id) ON DELETE CASCADE,
    broker TEXT NOT NULL CHECK (broker IN ('alpaca')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
