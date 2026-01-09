// Copyright 2024 ARL-Infra Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
    input String,
    stdout String,
    stderr String,
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
