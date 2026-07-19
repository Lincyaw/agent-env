mod checkpoint;
mod path_security;
mod pty_util;
mod executor;

use clap::Parser;
use std::path::PathBuf;
use std::sync::Arc;

#[derive(Parser)]
#[command(name = "executor-agent")]
struct Cli {
    /// Unix socket path
    #[arg(long = "socket", default_value = "/var/run/arl/exec.sock")]
    socket: PathBuf,

    /// Default workspace directory
    #[arg(long = "workspace", default_value = "/workspace")]
    workspace: PathBuf,

    /// iroh endpoint address file
    #[arg(long = "iroh-addr-file", default_value = "/var/run/arl/iroh-addr")]
    iroh_addr_file: PathBuf,

    /// Overlay checkpoint scratch directory (empty = disabled)
    #[arg(long = "checkpoint-dir", default_value = "")]
    checkpoint_dir: String,

    /// TCP listen port (0 = disabled)
    #[arg(long = "tcp-port", default_value_t = 0)]
    tcp_port: u16,
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

    let checkpoint_dir = if cli.checkpoint_dir.is_empty() {
        if std::env::var("ARL_CHECKPOINT_ENABLED").as_deref() == Ok("1") {
            "/mnt/arl-checkpoint".to_string()
        } else {
            String::new()
        }
    } else {
        cli.checkpoint_dir.clone()
    };

    let checkpointer = if checkpoint_dir.is_empty() {
        None
    } else {
        log::info!("checkpoint enabled, base_dir={checkpoint_dir}");
        Some(Arc::new(checkpoint::Checkpointer::new(PathBuf::from(&checkpoint_dir))))
    };

    log::info!("starting executor-agent");
    let socket = cli.socket.to_string_lossy().to_string();
    let workspace = cli.workspace.to_string_lossy().to_string();

    // Start iroh QUIC endpoint
    let iroh_workspace = workspace.clone();
    let iroh_addr_file = cli.iroh_addr_file.clone();
    let iroh_handle = tokio::spawn(async move {
        match executor::iroh_endpoint::IrohEndpoint::new(iroh_addr_file).await {
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
    let unix_workspace = workspace.clone();
    let unix_checkpointer = checkpointer.clone();
    let agent = executor::agent::Agent::new(socket, unix_workspace, unix_checkpointer);
    let (_tx, rx) = tokio::sync::watch::channel(false);
    let unix_handle = tokio::task::spawn_blocking(move || agent.run(rx));

    // Optionally start TCP listener
    let tcp_handle = if cli.tcp_port > 0 {
        let tcp_workspace = workspace.clone();
        let tcp_checkpointer = checkpointer.clone();
        let tcp_port = cli.tcp_port;
        Some(tokio::task::spawn_blocking(move || {
            use std::net::TcpListener;
            let addr = format!("0.0.0.0:{tcp_port}");
            let listener = match TcpListener::bind(&addr) {
                Ok(l) => l,
                Err(e) => {
                    log::error!("TCP bind on {addr} failed: {e}");
                    return;
                }
            };
            log::info!("executor-agent TCP listening on {addr}");
            loop {
                match listener.accept() {
                    Ok((stream, peer)) => {
                        stream.set_nodelay(true).ok();
                        log::info!("TCP connection from {peer}");
                        let ws = tcp_workspace.clone();
                        let ckpt = tcp_checkpointer.clone();
                        let (_tx, sd) = tokio::sync::watch::channel(false);
                        std::thread::spawn(move || {
                            if let Err(e) = executor::agent::handle_conn_tcp(stream, &ws, sd, ckpt) {
                                log::error!("TCP connection error: {e}");
                            }
                        });
                    }
                    Err(e) => {
                        log::error!("TCP accept error: {e}");
                        continue;
                    }
                }
            }
        }))
    } else {
        None
    };

    // Wait for any listener to finish
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
        _ = async {
            match tcp_handle {
                Some(h) => { let _ = h.await; }
                None => std::future::pending::<()>().await,
            }
        } => {
            log::info!("TCP listener stopped");
        }
    }
}
