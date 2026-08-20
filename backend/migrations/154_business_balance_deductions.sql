-- Agent Chat business deductions are written by the legacy integration service
-- directly into this database. Keep this ledger independent from gateway usage logs.
CREATE TABLE IF NOT EXISTS business_balance_deductions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    business_type VARCHAR(64) NOT NULL,
    request_id VARCHAR(128) NOT NULL,
    amount NUMERIC(20, 8) NOT NULL,
    balance_before NUMERIC(20, 8) NOT NULL,
    balance_after NUMERIC(20, 8) NOT NULL,
    notes TEXT NOT NULL,
    model VARCHAR(200) NOT NULL,
    employee_id VARCHAR(100) NOT NULL,
    input_tokens BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT business_balance_deductions_user_request_unique
        UNIQUE (user_id, request_id)
);

ALTER TABLE business_balance_deductions
    ADD COLUMN IF NOT EXISTS input_tokens BIGINT NOT NULL DEFAULT 0;

ALTER TABLE business_balance_deductions
    ADD COLUMN IF NOT EXISTS output_tokens BIGINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS business_balance_deductions_user_employee_idx
    ON business_balance_deductions (user_id, employee_id, created_at);

CREATE INDEX IF NOT EXISTS business_balance_deductions_business_type_idx
    ON business_balance_deductions (business_type);
