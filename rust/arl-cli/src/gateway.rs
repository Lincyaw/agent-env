use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};

#[derive(Clone)]
pub struct Client {
    base_url: String,
    http: reqwest::Client,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct SessionInfo {
    pub id: String,
    #[serde(default, rename = "sandboxName")]
    pub sandbox_name: String,
    #[serde(default)]
    pub image: String,
    #[serde(default)]
    pub profile: String,
    #[serde(default)]
    pub status: String,
    #[serde(default, rename = "podIP")]
    pub pod_ip: String,
    #[serde(default, rename = "podName")]
    pub pod_name: String,
    #[serde(default, rename = "irohAddr")]
    pub iroh_addr: String,
    #[serde(default, rename = "experimentId")]
    pub experiment_id: String,
    #[serde(default, rename = "createdAt")]
    pub created_at: String,
}

impl SessionInfo {
    pub fn age_human(&self) -> String {
        chrono_age(&self.created_at).unwrap_or_else(|| "-".into())
    }
}

fn chrono_age(ts: &str) -> Option<String> {
    let created: chrono::DateTime<chrono::Utc> = ts.parse().ok()?;
    let elapsed = chrono::Utc::now() - created;
    let secs = elapsed.num_seconds();
    if secs < 60 {
        Some(format!("{secs}s"))
    } else if secs < 3600 {
        Some(format!("{}m", secs / 60))
    } else {
        Some(format!("{}h{}m", secs / 3600, (secs % 3600) / 60))
    }
}

#[derive(Debug, Serialize, Deserialize)]
pub struct SummaryInfo {
    #[serde(default)]
    pub sessions: i64,
    #[serde(default, rename = "managedSessions")]
    pub managed_sessions: i64,
    #[serde(default)]
    pub pools: i64,
    #[serde(default, rename = "readyReplicas")]
    pub ready_replicas: i64,
    #[serde(default, rename = "allocatedReplicas")]
    pub allocated_replicas: i64,
    #[serde(default)]
    pub experiments: i64,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct ExperimentInfo {
    #[serde(default, rename = "experimentId")]
    pub experiment_id: String,
    #[serde(default, rename = "sessionCount")]
    pub session_count: i64,
    #[serde(default)]
    pub image: String,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct PoolInfo {
    #[serde(default)]
    pub name: String,
    #[serde(default)]
    pub image: String,
    #[serde(default)]
    pub state: String,
    #[serde(default)]
    pub replicas: i64,
    #[serde(default, rename = "readyReplicas")]
    pub ready_replicas: i64,
    #[serde(default, rename = "allocatedReplicas")]
    pub allocated_replicas: i64,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct ExecOutput {
    #[serde(default)]
    pub stdout: String,
    #[serde(default)]
    pub stderr: String,
    #[serde(default)]
    pub exit_code: i32,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct UploadResult {
    #[serde(default)]
    pub path: String,
    #[serde(default, rename = "bytesWritten")]
    pub bytes_written: i64,
    #[serde(default)]
    pub sha256: String,
}

impl Client {
    pub fn new(base_url: &str) -> Self {
        Self {
            base_url: base_url.trim_end_matches('/').to_string(),
            http: reqwest::Client::new(),
        }
    }

    pub async fn health(&self) -> Result<()> {
        let resp = self
            .http
            .get(format!("{}/healthz", self.base_url))
            .send()
            .await?;
        if resp.status().is_success() {
            Ok(())
        } else {
            anyhow::bail!("gateway unhealthy: {}", resp.status())
        }
    }

    pub async fn summary(&self) -> Result<SummaryInfo> {
        let resp = self
            .http
            .get(format!("{}/v1/summary", self.base_url))
            .send()
            .await?
            .error_for_status()?;
        Ok(resp.json().await?)
    }

    pub async fn create_managed_session(
        &self,
        image: &str,
        experiment_id: &str,
    ) -> Result<SessionInfo> {
        let body = serde_json::json!({
            "image": image,
            "experimentId": experiment_id,
        });
        let resp = self
            .http
            .post(format!("{}/v1/managed/sessions", self.base_url))
            .json(&body)
            .send()
            .await?;
        let status = resp.status();
        if !status.is_success() {
            let text = resp.text().await.unwrap_or_default();
            anyhow::bail!("create session failed ({}): {}", status, text);
        }
        Ok(resp.json().await?)
    }

    pub async fn get_session(&self, id: &str) -> Result<SessionInfo> {
        let resp = self
            .http
            .get(format!("{}/v1/sessions/{id}", self.base_url))
            .send()
            .await?
            .error_for_status()
            .context("get session")?;
        Ok(resp.json().await?)
    }

    pub async fn list_sessions(&self, experiment: Option<&str>) -> Result<Vec<SessionInfo>> {
        let mut url = format!("{}/v1/sessions", self.base_url);
        if let Some(exp) = experiment {
            url = format!("{}/v1/experiments/{exp}/sessions", self.base_url);
        }
        let resp = self.http.get(&url).send().await?.error_for_status()?;
        Ok(resp.json().await?)
    }

    pub async fn delete_session(&self, id: &str) -> Result<()> {
        self.http
            .delete(format!("{}/v1/sessions/{id}", self.base_url))
            .send()
            .await?
            .error_for_status()?;
        Ok(())
    }

    pub async fn delete_experiment(&self, id: &str) -> Result<()> {
        self.http
            .delete(format!("{}/v1/managed/experiments/{id}", self.base_url))
            .send()
            .await?
            .error_for_status()?;
        Ok(())
    }

    pub async fn list_experiments(&self) -> Result<Vec<ExperimentInfo>> {
        let resp = self
            .http
            .get(format!("{}/v1/managed/experiments", self.base_url))
            .send()
            .await?
            .error_for_status()?;
        Ok(resp.json().await?)
    }

    pub async fn list_experiment_sessions(&self, id: &str) -> Result<Vec<SessionInfo>> {
        let resp = self
            .http
            .get(format!(
                "{}/v1/managed/experiments/{id}/sessions",
                self.base_url
            ))
            .send()
            .await?
            .error_for_status()?;
        Ok(resp.json().await?)
    }

    pub async fn list_pools(&self) -> Result<Vec<PoolInfo>> {
        let resp = self
            .http
            .get(format!("{}/v1/pools", self.base_url))
            .send()
            .await?
            .error_for_status()?;
        Ok(resp.json().await?)
    }

    pub async fn get_pool(&self, name: &str) -> Result<PoolInfo> {
        let resp = self
            .http
            .get(format!("{}/v1/pools/{name}", self.base_url))
            .send()
            .await?
            .error_for_status()?;
        Ok(resp.json().await?)
    }

    pub async fn create_pool(&self, name: &str, image: &str, replicas: i32) -> Result<()> {
        let body = serde_json::json!({
            "name": name,
            "image": image,
            "replicas": replicas,
        });
        self.http
            .post(format!("{}/v1/pools", self.base_url))
            .json(&body)
            .send()
            .await?
            .error_for_status()?;
        Ok(())
    }

    pub async fn scale_pool(&self, name: &str, replicas: i32) -> Result<()> {
        let body = serde_json::json!({"replicas": replicas});
        self.http
            .patch(format!("{}/v1/pools/{name}", self.base_url))
            .json(&body)
            .send()
            .await?
            .error_for_status()?;
        Ok(())
    }

    pub async fn delete_pool(&self, name: &str) -> Result<()> {
        self.http
            .delete(format!("{}/v1/pools/{name}", self.base_url))
            .send()
            .await?
            .error_for_status()?;
        Ok(())
    }

    pub async fn get_logs(&self, session_id: &str) -> Result<String> {
        let resp = self
            .http
            .get(format!(
                "{}/v1/sessions/{session_id}/logs?tail=100",
                self.base_url
            ))
            .send()
            .await?
            .error_for_status()?;
        Ok(resp.text().await?)
    }

    pub async fn execute_http(
        &self,
        session_id: &str,
        command: &[String],
    ) -> Result<ExecOutput> {
        let body = serde_json::json!({
            "steps": [{
                "name": command.join(" "),
                "command": command,
            }],
        });
        let resp = self
            .http
            .post(format!(
                "{}/v1/sessions/{session_id}/execute",
                self.base_url
            ))
            .json(&body)
            .send()
            .await?
            .error_for_status()
            .context("execute")?;

        let data: serde_json::Value = resp.json().await?;
        let result = &data["results"][0]["output"];
        Ok(ExecOutput {
            stdout: result["stdout"].as_str().unwrap_or("").to_string(),
            stderr: result["stderr"].as_str().unwrap_or("").to_string(),
            exit_code: result["exit_code"].as_i64().unwrap_or(0) as i32,
        })
    }

    pub async fn upload_http(
        &self,
        session_id: &str,
        path: &str,
        data: Vec<u8>,
    ) -> Result<UploadResult> {
        let body = reqwest::multipart::Form::new()
            .text("path", path.to_string())
            .part(
                "file",
                reqwest::multipart::Part::bytes(data).file_name("upload"),
            );
        let resp = self
            .http
            .post(format!(
                "{}/v1/sessions/{session_id}/files",
                self.base_url
            ))
            .multipart(body)
            .send()
            .await?
            .error_for_status()?;
        Ok(resp.json().await?)
    }

    pub async fn download_http(&self, session_id: &str, path: &str) -> Result<Vec<u8>> {
        let resp = self
            .http
            .get(format!(
                "{}/v1/sessions/{session_id}/files?path={path}",
                self.base_url
            ))
            .send()
            .await?
            .error_for_status()?;
        Ok(resp.bytes().await?.to_vec())
    }
}
