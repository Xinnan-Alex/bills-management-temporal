CREATE TABLE bill_line_items (
    id UUID PRIMARY KEY,
    bill_id UUID NOT NULL REFERENCES bills(id) ON DELETE CASCADE,
    amount_minor BIGINT NOT NULL,
    currency_code CHAR(3) NOT NULL,
    description TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_bill_line_items_bill_id ON bill_line_items(bill_id);
CREATE UNIQUE INDEX idx_bill_line_items_idempotency_unique ON bill_line_items(bill_id, idempotency_key);
