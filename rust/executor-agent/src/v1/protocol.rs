use base64::engine::general_purpose::STANDARD as BASE64;
use base64::Engine;
use serde::{Deserialize, Deserializer, Serialize, Serializer};
use std::collections::HashMap;

/// Request is the JSON-over-socket protocol request from sidecar to executor agent.
#[derive(Debug, Deserialize)]
pub struct Request {
    #[serde(default)]
    pub id: String,

    #[serde(default, rename = "type")]
    pub req_type: String,

    #[serde(default)]
    pub cmd: Vec<String>,

    #[serde(default)]
    pub env: HashMap<String, String>,

    #[serde(default)]
    pub workdir: String,

    #[serde(default)]
    pub timeout: i32,

    #[serde(default)]
    pub pid: i32,

    #[serde(default)]
    pub signal: String,

    #[serde(default)]
    pub data: String,

    #[serde(default)]
    pub rows: i32,

    #[serde(default)]
    pub cols: i32,

    #[serde(default)]
    pub path: String,

    #[serde(default, deserialize_with = "deserialize_bytes")]
    pub content: Vec<u8>,

    #[serde(default)]
    pub expected_sha256: String,
}

/// Response is the JSON-over-socket protocol response from executor agent to sidecar.
#[derive(Debug, Clone, Serialize)]
#[derive(Default)]
pub struct Response {
    pub id: String,

    #[serde(skip_serializing_if = "String::is_empty")]
    pub stdout: String,

    #[serde(skip_serializing_if = "String::is_empty")]
    pub stderr: String,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub exit_code: Option<i32>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub bytes_written: Option<i64>,

    #[serde(skip_serializing_if = "Option::is_none")]
    pub size_bytes: Option<i64>,

    #[serde(skip_serializing_if = "is_zero_i64")]
    pub offset: i64,

    #[serde(skip_serializing_if = "String::is_empty")]
    pub sha256: String,

    #[serde(
        skip_serializing_if = "Vec::is_empty",
        serialize_with = "serialize_bytes"
    )]
    pub content: Vec<u8>,

    #[serde(skip_serializing_if = "std::ops::Not::not")]
    pub done: bool,

    #[serde(skip_serializing_if = "String::is_empty")]
    pub error: String,
}


impl Response {
    pub fn error(id: &str, msg: String) -> Self {
        Self {
            id: id.to_string(),
            error: msg,
            done: true,
            ..Default::default()
        }
    }

    pub fn done(id: &str) -> Self {
        Self {
            id: id.to_string(),
            done: true,
            ..Default::default()
        }
    }
}

fn is_zero_i64(v: &i64) -> bool {
    *v == 0
}

/// Serialize Vec<u8> as base64 string (matching Go encoding/json []byte behavior).
fn serialize_bytes<S>(bytes: &[u8], serializer: S) -> Result<S::Ok, S::Error>
where
    S: Serializer,
{
    let encoded = BASE64.encode(bytes);
    serializer.serialize_str(&encoded)
}

/// Deserialize base64 string to Vec<u8> (matching Go encoding/json []byte behavior).
fn deserialize_bytes<'de, D>(deserializer: D) -> Result<Vec<u8>, D::Error>
where
    D: Deserializer<'de>,
{
    // Go's encoding/json accepts both null and base64 string for []byte fields.
    // null → nil (empty slice), string → decoded bytes.
    let opt: Option<String> = Option::deserialize(deserializer)?;
    match opt {
        None => Ok(Vec::new()),
        Some(s) if s.is_empty() => Ok(Vec::new()),
        Some(s) => BASE64
            .decode(&s)
            .map_err(serde::de::Error::custom),
    }
}
