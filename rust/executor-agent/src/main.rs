mod path_security;
mod pty_util;
mod v1;
mod v2;

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

    /// iroh endpoint address file (V2 only)
    #[arg(long = "iroh-addr-file", default_value = "/var/run/arl/iroh-addr")]
    iroh_addr_file: PathBuf,
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
            let agent = v1::agent::Agent::new(cli.socket, cli.workspace);
            if let Err(e) = agent.run().await {
                log::error!("Agent error: {}", e);
                std::process::exit(1);
            }
        }
        "v2" => {
            log::info!("starting executor-agent with V2 protocol");
            let socket = cli.socket.to_string_lossy().to_string();
            let workspace = cli.workspace.to_string_lossy().to_string();

            // Start iroh QUIC endpoint
            let iroh_workspace = workspace.clone();
            let iroh_addr_file = cli.iroh_addr_file.clone();
            let iroh_handle = tokio::spawn(async move {
                match v2::iroh_endpoint::IrohEndpoint::new(iroh_addr_file).await {
                    Ok(ep) => {
                        if let Err(e) = ep.serve(iroh_workspace).await {
                            log::error!("iroh endpoint error: {}", e);
                        }
                    }
                    Err(e) => {
                        log::error!("failed to start iroh endpoint: {}", e);
                    }
                }
            });

            // Start Unix socket listener (blocking)
            let agent = v2::agent::AgentV2::new(socket, workspace);
            let (_tx, rx) = tokio::sync::watch::channel(false);
            let unix_handle = tokio::task::spawn_blocking(move || agent.run(rx));

            // Wait for either to finish
            tokio::select! {
                result = unix_handle => {
                    match result {
                        Ok(Ok(())) => {}
                        Ok(Err(e)) => {
                            log::error!("Unix socket agent error: {}", e);
                            std::process::exit(1);
                        }
                        Err(e) => {
                            log::error!("Unix socket agent task error: {}", e);
                            std::process::exit(1);
                        }
                    }
                }
                _ = iroh_handle => {
                    log::info!("iroh endpoint stopped");
                }
            }
        }
        other => {
            log::error!("unknown protocol version: {} (expected v1 or v2)", other);
            std::process::exit(1);
        }
    }
}
