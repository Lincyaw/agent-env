use serde::{Deserialize, Serialize};
use std::path::PathBuf;

#[derive(Debug, Serialize, Deserialize)]
pub struct Config {
    #[serde(default = "default_gateway_url")]
    pub gateway_url: String,
    #[serde(default)]
    pub api_key: String,
}

fn default_gateway_url() -> String {
    std::env::var("ARL_GATEWAY_URL").unwrap_or_else(|_| "http://localhost:8080".into())
}

impl Default for Config {
    fn default() -> Self {
        Self {
            gateway_url: default_gateway_url(),
            api_key: String::new(),
        }
    }
}

impl Config {
    fn path() -> PathBuf {
        dirs::config_dir()
            .unwrap_or_else(|| PathBuf::from("."))
            .join("arl")
            .join("config.toml")
    }

    pub fn load() -> Self {
        let mut cfg: Config = std::fs::read_to_string(Self::path())
            .ok()
            .and_then(|c| toml::from_str(&c).ok())
            .unwrap_or_default();

        if let Ok(key) = std::env::var("ARL_API_KEY") {
            cfg.api_key = key;
        }
        cfg
    }

    pub fn save(&self) -> anyhow::Result<()> {
        let path = Self::path();
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        let content = toml::to_string_pretty(self)?;
        std::fs::write(&path, content)?;
        Ok(())
    }
}
