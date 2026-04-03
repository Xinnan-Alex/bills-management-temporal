CREATE TABLE bills (
    id UUID PRIMARY KEY,
    currency_code CHAR(3) NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('OPEN', 'CLOSED')),
    running_total_minor BIGINT NOT NULL DEFAULT 0,
    closed_total_minor BIGINT,
    closed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_bills_status ON bills(status);
CREATE INDEX idx_bills_created_at ON bills(created_at);
