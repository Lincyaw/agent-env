package audit

// Schema defines the ClickHouse table schemas for audit logging

// TaskAuditTableSQL is the SQL to create the task_audit table
const TaskAuditTableSQL = `
CREATE TABLE IF NOT EXISTS task_audit (
    trace_id String,
    namespace String,
    name String,
    sandbox_ref String,
    state String,
    exit_code Int32,
    duration String,
    start_time DateTime64(3),
    completion_time DateTime64(3),
    step_count Int32,
    timestamp DateTime64(3) DEFAULT now64(3)
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (timestamp, namespace, name)
TTL timestamp + INTERVAL 30 DAY
`

// SandboxAuditTableSQL is the SQL to create the sandbox_audit table
const SandboxAuditTableSQL = `
CREATE TABLE IF NOT EXISTS sandbox_audit (
    trace_id String,
    namespace String,
    name String,
    pool_ref String,
    phase String,
    pod_name String,
    event String,
    timestamp DateTime64(3) DEFAULT now64(3)
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (timestamp, namespace, name)
TTL timestamp + INTERVAL 30 DAY
`
