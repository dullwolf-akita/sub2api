-- Persist per-request gateway timing stages for troubleshooting and capacity analysis.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS timing_breakdown JSONB;

COMMENT ON COLUMN usage_logs.timing_breakdown IS
    'Gateway timing breakdown in milliseconds, keyed by request stage';
