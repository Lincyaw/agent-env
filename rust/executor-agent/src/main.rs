mod agent;
mod agent_v2;
mod path_security;
mod protocol;
mod protocol_v2;
mod pty_util;

use clap::Parser;
use std::path::PathBuf;

#[derive(Parser)]
#[command(name = "executor-agent")]
struct Cli {
    /// Unix socket path
    #[arg(long = "socket", default_value = "/var/run/arl/exec.sock")]
    socket: PathBuf,

    /// Default workspace directory
    #[arg(long = "workspace", default_value = "/workspace")]
    workspace: PathBuf,

    /// Protocol version (v1 or v2). Can also be set via EXECUTOR_PROTOCOL env var.
    #[arg(long = "protocol", default_value = "v1")]
    protocol: String,
}

#[tokio::main]
async fn main() {
    env_logger::init();

    let mut cli = Cli::parse();

    if let Ok(val) = std::env::var("EXECUTOR_PROTOCOL") {
        if !val.is_empty() {
            cli.protocol = val;
        }
    }

    if let Some(parent) = cli.socket.parent() {
        if let Err(e) = std::fs::create_dir_all(parent) {
            log::error!("Failed to create socket directory: {}", e);
            std::process::exit(1);
        }
    }

    if let Err(e) = std::fs::create_dir_all(&cli.workspace) {
        log::error!("Failed to create workspace directory: {}", e);
        std::process::exit(1);
    }

    match cli.protocol.as_str() {
        "v1" => {
            log::info!("starting executor-agent with V1 protocol");
            let agent = agent::Agent::new(cli.socket, cli.workspace);
            if let Err(e) = agent.run().await {
                log::error!("Agent error: {}", e);
                std::process::exit(1);
            }
        }
        "v2" => {
            log::info!("starting executor-agent with V2 protocol");
            let socket = cli.socket.to_string_lossy().to_string();
            let workspace = cli.workspace.to_string_lossy().to_string();
            let agent = agent_v2::AgentV2::new(socket, workspace);
            let (_tx, rx) = tokio::sync::watch::channel(false);
            // V2 agent is synchronous, run it in a blocking task
            let result = tokio::task::spawn_blocking(move || agent.run(rx)).await;
            match result {
                Ok(Ok(())) => {}
                Ok(Err(e)) => {
                    log::error!("Agent error: {}", e);
                    std::process::exit(1);
                }
                Err(e) => {
                    log::error!("Agent task error: {}", e);
                    std::process::exit(1);
                }
            }
        }
        other => {
            log::error!("unknown protocol version: {} (expected v1 or v2)", other);
            std::process::exit(1);
        }
    }
}
