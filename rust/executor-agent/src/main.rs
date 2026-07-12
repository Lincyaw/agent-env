mod agent;
mod path_security;
mod protocol;
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
}

#[tokio::main]
async fn main() {
    env_logger::init();

    let cli = Cli::parse();

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

    let agent = agent::Agent::new(cli.socket, cli.workspace);
    if let Err(e) = agent.run().await {
        log::error!("Agent error: {}", e);
        std::process::exit(1);
    }
}
