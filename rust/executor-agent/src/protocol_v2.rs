use serde::{Deserialize, Serialize};
use std::collections::HashMap;

// ---------------------------------------------------------------------------
// Requests (client -> server)
// ---------------------------------------------------------------------------

/// V2 request envelope. Tagged by the `method` field.
#[derive(Debug, Deserialize)]
#[serde(tag = "method", rename_all = "snake_case")]
pub enum V2Request {
    Spawn {
        id: String,
        params: SpawnParams,
    },
    Stdin {
        id: String,
        params: StdinParams,
    },
    Signal {
        id: String,
        params: SignalParams,
    },
    Resize {
        id: String,
        params: ResizeParams,
    },
    WriteFile {
        id: String,
        params: WriteFileParams,
    },
    FileChunk {
        id: String,
        params: FileChunkParams,
    },
    FileDone {
        id: String,
    },
    ReadFile {
        id: String,
        params: ReadFileParams,
    },
    Stat {
        id: String,
        params: StatParams,
    },
    ListDir {
        id: String,
        params: ListDirParams,
    },
    Ping {
        id: String,
    },
}

impl V2Request {
    pub fn id(&self) -> &str {
        match self {
            Self::Spawn { id, .. } => id,
            Self::Stdin { id, .. } => id,
            Self::Signal { id, .. } => id,
            Self::Resize { id, .. } => id,
            Self::WriteFile { id, .. } => id,
            Self::FileChunk { id, .. } => id,
            Self::FileDone { id } => id,
            Self::ReadFile { id, .. } => id,
            Self::Stat { id, .. } => id,
            Self::ListDir { id, .. } => id,
            Self::Ping { id } => id,
        }
    }
}

#[derive(Debug, Deserialize)]
pub struct SpawnParams {
    pub cmd: Vec<String>,
    #[serde(default)]
    pub env: HashMap<String, String>,
    #[serde(default)]
    pub workdir: Option<String>,
    #[serde(default)]
    pub timeout: Option<u64>,
    #[serde(default)]
    pub pty: Option<PtyParams>,
    /// When true, the spawned process accepts stdin messages.
    #[serde(default)]
    pub stdin: bool,
}

#[derive(Debug, Deserialize)]
pub struct PtyParams {
    #[serde(default = "default_rows")]
    pub rows: u16,
    #[serde(default = "default_cols")]
    pub cols: u16,
}

fn default_rows() -> u16 {
    24
}
fn default_cols() -> u16 {
    80
}

#[derive(Debug, Deserialize)]
pub struct StdinParams {
    pub handle: String,
    pub data: String,
}

#[derive(Debug, Deserialize)]
pub struct SignalParams {
    pub handle: String,
    pub signal: String,
}

#[derive(Debug, Deserialize)]
pub struct ResizeParams {
    pub handle: String,
    pub rows: u16,
    pub cols: u16,
}

#[derive(Debug, Deserialize)]
pub struct WriteFileParams {
    pub path: String,
    #[serde(default)]
    pub sha256: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct FileChunkParams {
    pub content: String, // base64-encoded
}

#[derive(Debug, Deserialize)]
pub struct ReadFileParams {
    pub path: String,
}

#[derive(Debug, Deserialize)]
pub struct StatParams {
    pub path: String,
}

#[derive(Debug, Deserialize)]
pub struct ListDirParams {
    pub path: String,
    #[serde(default)]
    pub recursive: bool,
}

// ---------------------------------------------------------------------------
// Responses (server -> client, correlated by id)
// ---------------------------------------------------------------------------

/// Successful response wrapper.
#[derive(Debug, Serialize)]
pub struct V2Response {
    pub id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<V2Error>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub chunk: Option<serde_json::Value>,
}

impl V2Response {
    pub fn ok(id: String) -> Self {
        Self {
            id,
            result: Some(serde_json::json!({})),
            error: None,
            chunk: None,
        }
    }

    pub fn result(id: String, value: serde_json::Value) -> Self {
        Self {
            id,
            result: Some(value),
            error: None,
            chunk: None,
        }
    }

    pub fn err(id: String, code: &str, message: String) -> Self {
        Self {
            id,
            result: None,
            error: Some(V2Error {
                code: code.to_string(),
                message,
            }),
            chunk: None,
        }
    }

    pub fn chunk(id: String, value: serde_json::Value) -> Self {
        Self {
            id,
            result: None,
            error: None,
            chunk: Some(value),
        }
    }
}

#[derive(Debug, Serialize)]
pub struct V2Error {
    pub code: String,
    pub message: String,
}

// ---------------------------------------------------------------------------
// Events (server -> client, proactive push, no request id)
// ---------------------------------------------------------------------------

#[derive(Debug, Serialize)]
#[serde(tag = "event", rename_all = "snake_case")]
pub enum V2Event {
    Stdout { handle: String, data: String },
    Stderr { handle: String, data: String },
    Exit { handle: String, exit_code: i32 },
}

// ---------------------------------------------------------------------------
// Result payloads
// ---------------------------------------------------------------------------

#[derive(Debug, Serialize)]
pub struct SpawnResult {
    pub handle: String,
}

#[derive(Debug, Serialize)]
pub struct WriteFileResult {
    pub bytes_written: i64,
    pub sha256: String,
}

#[derive(Debug, Serialize)]
pub struct ReadFileChunk {
    pub offset: i64,
    pub content: String, // base64-encoded
}

#[derive(Debug, Serialize)]
pub struct ReadFileResult {
    pub size_bytes: i64,
    pub sha256: String,
}

#[derive(Debug, Serialize)]
pub struct StatResult {
    pub exists: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub is_dir: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub size: Option<u64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub mode: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub modified: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct ListDirResult {
    pub entries: Vec<DirEntry>,
}

#[derive(Debug, Serialize)]
pub struct DirEntry {
    pub name: String,
    pub is_dir: bool,
    pub size: u64,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_deserialize_ping() {
        let json = r#"{"id":"r1","method":"ping"}"#;
        let req: V2Request = serde_json::from_str(json).unwrap();
        assert_eq!(req.id(), "r1");
        assert!(matches!(req, V2Request::Ping { .. }));
    }

    #[test]
    fn test_deserialize_spawn_minimal() {
        let json = r#"{"id":"r2","method":"spawn","params":{"cmd":["ls"]}}"#;
        let req: V2Request = serde_json::from_str(json).unwrap();
        if let V2Request::Spawn { params, .. } = req {
            assert_eq!(params.cmd, vec!["ls"]);
            assert!(params.pty.is_none());
            assert!(!params.stdin);
        } else {
            panic!("expected Spawn");
        }
    }

    #[test]
    fn test_deserialize_spawn_with_pty() {
        let json = r#"{
            "id": "r3",
            "method": "spawn",
            "params": {
                "cmd": ["bash"],
                "pty": {"rows": 50, "cols": 120},
                "stdin": true
            }
        }"#;
        let req: V2Request = serde_json::from_str(json).unwrap();
        if let V2Request::Spawn { params, .. } = req {
            let pty = params.pty.unwrap();
            assert_eq!(pty.rows, 50);
            assert_eq!(pty.cols, 120);
            assert!(params.stdin);
        } else {
            panic!("expected Spawn");
        }
    }

    #[test]
    fn test_deserialize_stdin() {
        let json = r#"{"id":"r4","method":"stdin","params":{"handle":"proc-abc","data":"y\n"}}"#;
        let req: V2Request = serde_json::from_str(json).unwrap();
        if let V2Request::Stdin { params, .. } = req {
            assert_eq!(params.handle, "proc-abc");
        } else {
            panic!("expected Stdin");
        }
    }

    #[test]
    fn test_deserialize_signal() {
        let json = r#"{"id":"r5","method":"signal","params":{"handle":"proc-abc","signal":"SIGINT"}}"#;
        let req: V2Request = serde_json::from_str(json).unwrap();
        assert!(matches!(req, V2Request::Signal { .. }));
    }

    #[test]
    fn test_deserialize_write_file() {
        let json = r#"{"id":"r6","method":"write_file","params":{"path":"out/f.bin"}}"#;
        let req: V2Request = serde_json::from_str(json).unwrap();
        assert!(matches!(req, V2Request::WriteFile { .. }));
    }

    #[test]
    fn test_deserialize_read_file() {
        let json = r#"{"id":"r7","method":"read_file","params":{"path":"out/f.bin"}}"#;
        let req: V2Request = serde_json::from_str(json).unwrap();
        assert!(matches!(req, V2Request::ReadFile { .. }));
    }

    #[test]
    fn test_deserialize_stat() {
        let json = r#"{"id":"r8","method":"stat","params":{"path":"output/"}}"#;
        let req: V2Request = serde_json::from_str(json).unwrap();
        assert!(matches!(req, V2Request::Stat { .. }));
    }

    #[test]
    fn test_deserialize_list_dir() {
        let json = r#"{"id":"r9","method":"list_dir","params":{"path":"output/","recursive":true}}"#;
        let req: V2Request = serde_json::from_str(json).unwrap();
        if let V2Request::ListDir { params, .. } = req {
            assert!(params.recursive);
        } else {
            panic!("expected ListDir");
        }
    }

    #[test]
    fn test_serialize_response_ok() {
        let resp = V2Response::ok("r1".into());
        let json = serde_json::to_string(&resp).unwrap();
        assert!(json.contains("\"result\""));
        assert!(!json.contains("\"error\""));
    }

    #[test]
    fn test_serialize_response_err() {
        let resp = V2Response::err("r1".into(), "SPAWN_FAILED", "not found".into());
        let json = serde_json::to_string(&resp).unwrap();
        assert!(json.contains("SPAWN_FAILED"));
        assert!(!json.contains("\"result\""));
    }

    #[test]
    fn test_serialize_event_stdout() {
        let ev = V2Event::Stdout {
            handle: "proc-abc".into(),
            data: "hello".into(),
        };
        let json = serde_json::to_string(&ev).unwrap();
        assert!(json.contains("\"event\":\"stdout\""));
        assert!(json.contains("proc-abc"));
    }

    #[test]
    fn test_serialize_event_exit() {
        let ev = V2Event::Exit {
            handle: "proc-abc".into(),
            exit_code: 0,
        };
        let json = serde_json::to_string(&ev).unwrap();
        assert!(json.contains("\"event\":\"exit\""));
        assert!(json.contains("\"exit_code\":0"));
    }
}
