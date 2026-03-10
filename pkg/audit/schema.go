package audit

// SessionStepAuditTableSQL creates the session_step_audit table (replaces task_audit)
const SessionStepAuditTableSQL = `
CREATE TABLE IF NOT EXISTS session_step_audit (
    session_id String,
    trace_id String,
    namespace String,
    step_index Int32,
    step_name String,
    step_type String,
    input String,
    stdout String,
    stderr String,
    exit_code Int32,
    snapshot_id String,
    duration_ms Int64,
    timestamp DateTime64(3),
    created_at DateTime64(3) DEFAULT now64(3)
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(created_at)
ORDER BY (created_at, session_id, step_index)
TTL toDateTime(created_at) + INTERVAL 30 DAY
`
