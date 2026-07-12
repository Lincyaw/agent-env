mod config;
mod gateway;
mod proto;
mod transport;

use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "arl", version, about = "ARL — Agentic RL runtime CLI")]
struct Cli {
    #[command(subcommand)]
    command: Commands,

    /// Gateway API base URL
    #[arg(short, long, global = true, env = "ARL_GATEWAY_URL")]
    gateway_url: Option<String>,

    /// Output format: text, json
    #[arg(long, global = true, default_value = "text")]
    format: String,
}

#[derive(Subcommand)]
enum Commands {
    /// Execute a command in a new ephemeral sandbox (create → exec → destroy)
    Run {
        /// Container image
        image: String,
        /// Command to execute
        #[arg(last = true)]
        command: Vec<String>,
        /// Experiment ID (optional grouping)
        #[arg(short, long)]
        experiment: Option<String>,
    },

    /// Create a persistent session
    Create {
        /// Container image
        image: String,
        /// Human-friendly alias
        #[arg(short, long)]
        name: Option<String>,
        /// Experiment ID
        #[arg(short, long)]
        experiment: Option<String>,
        /// Idle timeout (e.g. "1h", "30m")
        #[arg(long)]
        idle_timeout: Option<String>,
    },

    /// Attach to a persistent session (interactive shell)
    Attach {
        /// Session ID or name
        session: String,
    },

    /// Execute a command in a persistent session
    Exec {
        /// Session ID or name
        session: String,
        /// Command to execute
        #[arg(last = true)]
        command: Vec<String>,
    },

    /// Forward a local port to a remote port inside the sandbox
    Tunnel {
        /// Session ID or name
        session: String,
        /// Remote port inside the sandbox
        remote_port: u16,
        /// Local port to listen on (default: same as remote)
        #[arg(short, long)]
        local: Option<u16>,
        /// Remote host inside the sandbox
        #[arg(long, default_value = "127.0.0.1")]
        remote_host: String,
    },

    /// Copy files between local machine and sandbox
    Cp {
        /// Source: "session:path" or local path
        src: String,
        /// Destination: "session:path" or local path
        dst: String,
    },

    /// List active sessions
    Ps {
        /// Include stopped sessions
        #[arg(short, long)]
        all: bool,
        /// Filter by experiment
        #[arg(short, long)]
        experiment: Option<String>,
    },

    /// Show session or pod logs
    Logs {
        /// Session ID or name
        session: String,
        /// Follow log output
        #[arg(short, long)]
        follow: bool,
        /// Number of recent lines
        #[arg(long, default_value = "100")]
        tail: usize,
    },

    /// Destroy a session
    Kill {
        /// Session ID(s) or name(s)
        sessions: Vec<String>,
    },

    /// Show gateway health and summary
    Status,

    /// Manage experiments
    #[command(subcommand)]
    Exp(ExpCommands),

    /// Manage warm pools
    #[command(subcommand)]
    Pool(PoolCommands),

    /// Manage CLI configuration
    #[command(subcommand)]
    Config(ConfigCommands),
}

#[derive(Subcommand)]
enum ExpCommands {
    /// Create an experiment with N sessions
    Create {
        /// Experiment ID
        id: String,
        /// Container image
        #[arg(short, long)]
        image: String,
        /// Number of sessions
        #[arg(short = 'n', long, default_value = "1")]
        sessions: u32,
        /// Resource profile
        #[arg(short, long, default_value = "default")]
        profile: String,
    },
    /// List experiments
    Ls,
    /// List sessions for an experiment
    Sessions {
        /// Experiment ID
        id: String,
    },
    /// Delete all sessions for an experiment
    Rm {
        /// Experiment ID
        id: String,
        /// Skip confirmation
        #[arg(short, long)]
        force: bool,
    },
}

#[derive(Subcommand)]
enum PoolCommands {
    /// List warm pools
    Ls {
        /// Include stopped pools
        #[arg(short, long)]
        all: bool,
    },
    /// Get pool details
    Get {
        /// Pool name
        name: String,
    },
    /// Create a warm pool
    Create {
        /// Pool name
        name: String,
        /// Container image
        #[arg(short, long)]
        image: String,
        /// Number of warm replicas
        #[arg(short, long, default_value = "2")]
        replicas: i32,
        /// Wait for ready
        #[arg(short, long)]
        wait: bool,
    },
    /// Scale pool replicas
    Scale {
        /// Pool name
        name: String,
        /// Target replica count
        #[arg(short, long)]
        replicas: i32,
    },
    /// Delete a pool
    Rm {
        /// Pool name
        name: String,
        /// Skip confirmation
        #[arg(short, long)]
        force: bool,
    },
}

#[derive(Subcommand)]
enum ConfigCommands {
    /// Show current configuration
    Show,
    /// Set a configuration value
    Set {
        /// Key (e.g. "gateway", "context")
        key: String,
        /// Value
        value: String,
    },
}

#[tokio::main]
async fn main() {
    let cli = Cli::parse();

    let cfg = config::Config::load();
    let gateway_url = cli
        .gateway_url
        .unwrap_or_else(|| cfg.gateway_url.clone());
    let json_output = cli.format == "json";

    let result = match cli.command {
        Commands::Run {
            image,
            command,
            experiment,
        } => cmd_run(&gateway_url, &image, &command, experiment.as_deref(), json_output).await,

        Commands::Create {
            image,
            name,
            experiment,
            idle_timeout,
        } => cmd_create(&gateway_url, &image, name.as_deref(), experiment.as_deref(), idle_timeout.as_deref(), json_output).await,

        Commands::Attach { session } => cmd_attach(&gateway_url, &session, json_output).await,

        Commands::Exec { session, command } => {
            cmd_exec(&gateway_url, &session, &command, json_output).await
        }

        Commands::Tunnel {
            session,
            remote_port,
            local,
            remote_host,
        } => {
            cmd_tunnel(
                &gateway_url,
                &session,
                &remote_host,
                remote_port,
                local.unwrap_or(remote_port),
            )
            .await
        }

        Commands::Cp { src, dst } => cmd_cp(&gateway_url, &src, &dst, json_output).await,

        Commands::Ps { all, experiment } => {
            cmd_ps(&gateway_url, all, experiment.as_deref(), json_output).await
        }

        Commands::Logs {
            session,
            follow,
            tail,
        } => cmd_logs(&gateway_url, &session, follow, tail).await,

        Commands::Kill { sessions } => cmd_kill(&gateway_url, &sessions, json_output).await,

        Commands::Status => cmd_status(&gateway_url, json_output).await,

        Commands::Exp(sub) => cmd_exp(&gateway_url, sub, json_output).await,
        Commands::Pool(sub) => cmd_pool(&gateway_url, sub, json_output).await,
        Commands::Config(sub) => cmd_config(sub),
    };

    if let Err(e) = result {
        eprintln!("error: {e}");
        std::process::exit(1);
    }
}

async fn cmd_run(
    gw: &str,
    image: &str,
    command: &[String],
    experiment: Option<&str>,
    json_output: bool,
) -> anyhow::Result<()> {
    let client = gateway::Client::new(gw);
    let exp_id = experiment
        .map(String::from)
        .unwrap_or_else(|| format!("run-{}", std::process::id()));

    let session = client.create_managed_session(image, &exp_id).await?;
    let sid = &session.id;

    let transport = transport::connect(gw, &session).await;

    let result = if command.is_empty() {
        transport.shell(sid).await
    } else {
        transport.exec(sid, command).await
    };

    let _ = client.delete_session(sid).await;
    let _ = client.delete_experiment(&exp_id).await;

    let output = result?;
    if json_output {
        println!("{}", serde_json::to_string_pretty(&output)?);
    } else {
        print!("{}", output.stdout);
        if !output.stderr.is_empty() {
            eprint!("{}", output.stderr);
        }
    }

    if output.exit_code != 0 {
        std::process::exit(output.exit_code);
    }
    Ok(())
}

async fn cmd_create(
    gw: &str,
    image: &str,
    _name: Option<&str>,
    experiment: Option<&str>,
    _idle_timeout: Option<&str>,
    json_output: bool,
) -> anyhow::Result<()> {
    let client = gateway::Client::new(gw);
    let exp_id = experiment
        .map(String::from)
        .unwrap_or_else(|| format!("session-{}", std::process::id()));

    let session = client.create_managed_session(image, &exp_id).await?;

    if json_output {
        println!("{}", serde_json::to_string_pretty(&session)?);
    } else {
        println!("{}", session.id);
        if !session.iroh_addr.is_empty() {
            eprintln!("transport: quic (iroh direct-connect)");
        } else {
            eprintln!("transport: http");
        }
    }
    Ok(())
}

async fn cmd_attach(gw: &str, session: &str, _json_output: bool) -> anyhow::Result<()> {
    let client = gateway::Client::new(gw);
    let info = client.get_session(session).await?;
    let transport = transport::connect(gw, &info).await;
    let output = transport.shell(&info.id).await?;
    if output.exit_code != 0 {
        std::process::exit(output.exit_code);
    }
    Ok(())
}

async fn cmd_exec(
    gw: &str,
    session: &str,
    command: &[String],
    json_output: bool,
) -> anyhow::Result<()> {
    if command.is_empty() {
        anyhow::bail!("no command specified");
    }
    let client = gateway::Client::new(gw);
    let info = client.get_session(session).await?;
    let transport = transport::connect(gw, &info).await;
    let output = transport.exec(&info.id, command).await?;

    if json_output {
        println!("{}", serde_json::to_string_pretty(&output)?);
    } else {
        print!("{}", output.stdout);
        if !output.stderr.is_empty() {
            eprint!("{}", output.stderr);
        }
    }
    if output.exit_code != 0 {
        std::process::exit(output.exit_code);
    }
    Ok(())
}

async fn cmd_tunnel(
    gw: &str,
    session: &str,
    remote_host: &str,
    remote_port: u16,
    local_port: u16,
) -> anyhow::Result<()> {
    let client = gateway::Client::new(gw);
    let info = client.get_session(session).await?;

    if info.iroh_addr.is_empty() {
        anyhow::bail!("tunnel requires iroh direct-connect (session has no iroh address)");
    }

    let quic = transport::QuicTransport::connect(&info.iroh_addr).await?;
    eprintln!(
        "forwarding 127.0.0.1:{local_port} → {remote_host}:{remote_port} (quic tunnel)"
    );
    quic.tunnel_forward(remote_host, remote_port, local_port)
        .await
}

async fn cmd_cp(
    gw: &str,
    src: &str,
    dst: &str,
    json_output: bool,
) -> anyhow::Result<()> {
    let (session_id, remote_path, local_path, is_upload) = parse_cp_args(src, dst)?;
    let client = gateway::Client::new(gw);
    let info = client.get_session(&session_id).await?;
    let transport = transport::connect(gw, &info).await;

    if is_upload {
        let data = std::fs::read(&local_path)?;
        let result = transport.upload(&info.id, &remote_path, &data).await?;
        if json_output {
            println!("{}", serde_json::to_string_pretty(&result)?);
        } else {
            eprintln!("uploaded {} ({} bytes)", remote_path, result.bytes_written);
        }
    } else {
        let data = transport.download(&info.id, &remote_path).await?;
        std::fs::write(&local_path, &data)?;
        if !json_output {
            eprintln!("downloaded {} → {} ({} bytes)", remote_path, local_path, data.len());
        }
    }
    Ok(())
}

fn parse_cp_args(src: &str, dst: &str) -> anyhow::Result<(String, String, String, bool)> {
    if let Some((sid, path)) = src.split_once(':') {
        Ok((sid.to_string(), path.to_string(), dst.to_string(), false))
    } else if let Some((sid, path)) = dst.split_once(':') {
        Ok((sid.to_string(), path.to_string(), src.to_string(), true))
    } else {
        anyhow::bail!("one of src/dst must be <session>:<path>");
    }
}

async fn cmd_ps(
    gw: &str,
    _all: bool,
    experiment: Option<&str>,
    json_output: bool,
) -> anyhow::Result<()> {
    let client = gateway::Client::new(gw);
    let sessions = client.list_sessions(experiment).await?;

    if json_output {
        println!("{}", serde_json::to_string_pretty(&sessions)?);
        return Ok(());
    }

    if sessions.is_empty() {
        println!("No sessions.");
        return Ok(());
    }

    println!(
        "{:<42} {:<12} {:<25} {:<10}",
        "ID", "STATUS", "IMAGE", "AGE"
    );
    for s in &sessions {
        let img = s.image.split('/').last().unwrap_or(&s.image);
        let img = if img.len() > 24 { &img[..24] } else { img };
        println!(
            "{:<42} {:<12} {:<25} {:<10}",
            s.id, s.status, img, s.age_human()
        );
    }
    Ok(())
}

async fn cmd_logs(
    gw: &str,
    session: &str,
    _follow: bool,
    _tail: usize,
) -> anyhow::Result<()> {
    let client = gateway::Client::new(gw);
    let logs = client.get_logs(session).await?;
    print!("{logs}");
    Ok(())
}

async fn cmd_kill(gw: &str, sessions: &[String], json_output: bool) -> anyhow::Result<()> {
    let client = gateway::Client::new(gw);
    for sid in sessions {
        client.delete_session(sid).await?;
        if !json_output {
            println!("killed {sid}");
        }
    }
    Ok(())
}

async fn cmd_status(gw: &str, json_output: bool) -> anyhow::Result<()> {
    let client = gateway::Client::new(gw);
    let health = client.health().await;
    let summary = client.summary().await;

    if json_output {
        let mut m = serde_json::Map::new();
        m.insert("healthy".into(), health.is_ok().into());
        m.insert("gateway".into(), gw.into());
        if let Ok(s) = &summary {
            m.insert("sessions".into(), s.sessions.into());
            m.insert("pools".into(), s.pools.into());
        }
        println!("{}", serde_json::to_string_pretty(&m)?);
        return Ok(());
    }

    match health {
        Ok(()) => println!("gateway:   ok ({})", gw),
        Err(e) => println!("gateway:   UNHEALTHY ({})", e),
    }
    if let Ok(s) = summary {
        println!(
            "sessions:  {} ({} managed)",
            s.sessions, s.managed_sessions
        );
        println!(
            "pools:     {} (ready={}, allocated={})",
            s.pools, s.ready_replicas, s.allocated_replicas
        );
        println!("experiments: {}", s.experiments);
    }
    Ok(())
}

async fn cmd_exp(gw: &str, sub: ExpCommands, json_output: bool) -> anyhow::Result<()> {
    let client = gateway::Client::new(gw);
    match sub {
        ExpCommands::Create {
            id,
            image,
            sessions: count,
            profile: _,
        } => {
            let mut created = Vec::new();
            for _ in 0..count {
                let s = client.create_managed_session(&image, &id).await?;
                created.push(s);
            }
            if json_output {
                println!("{}", serde_json::to_string_pretty(&created)?);
            } else {
                println!("experiment {id}: created {} session(s)", created.len());
                for s in &created {
                    println!("  {}", s.id);
                }
            }
        }
        ExpCommands::Ls => {
            let exps = client.list_experiments().await?;
            if json_output {
                println!("{}", serde_json::to_string_pretty(&exps)?);
            } else if exps.is_empty() {
                println!("No experiments.");
            } else {
                println!("{:<30} {:<10} {:<25}", "EXPERIMENT", "SESSIONS", "IMAGE");
                for e in &exps {
                    println!("{:<30} {:<10} {:<25}", e.experiment_id, e.session_count, e.image);
                }
            }
        }
        ExpCommands::Sessions { id } => {
            let sessions = client.list_experiment_sessions(&id).await?;
            if json_output {
                println!("{}", serde_json::to_string_pretty(&sessions)?);
            } else if sessions.is_empty() {
                println!("No sessions for experiment {id}.");
            } else {
                for s in &sessions {
                    println!("{}", s.id);
                }
            }
        }
        ExpCommands::Rm { id, force } => {
            if !force {
                eprintln!("delete all sessions for experiment {id}? use --force");
                std::process::exit(1);
            }
            client.delete_experiment(&id).await?;
            if !json_output {
                println!("experiment {id} deleted.");
            }
        }
    }
    Ok(())
}

async fn cmd_pool(gw: &str, sub: PoolCommands, json_output: bool) -> anyhow::Result<()> {
    let client = gateway::Client::new(gw);
    match sub {
        PoolCommands::Ls { all: _ } => {
            let pools = client.list_pools().await?;
            if json_output {
                println!("{}", serde_json::to_string_pretty(&pools)?);
            } else if pools.is_empty() {
                println!("No pools.");
            } else {
                println!(
                    "{:<40} {:<10} {:<6} {:<6} {:<10}",
                    "NAME", "STATE", "READY", "ALLOC", "REPLICAS"
                );
                for p in &pools {
                    println!(
                        "{:<40} {:<10} {:<6} {:<6} {:<10}",
                        p.name, p.state, p.ready_replicas, p.allocated_replicas, p.replicas
                    );
                }
            }
        }
        PoolCommands::Get { name } => {
            let p = client.get_pool(&name).await?;
            if json_output {
                println!("{}", serde_json::to_string_pretty(&p)?);
            } else {
                println!("name:      {}", p.name);
                println!("image:     {}", p.image);
                println!("state:     {}", p.state);
                println!(
                    "replicas:  {} (ready={}, allocated={})",
                    p.replicas, p.ready_replicas, p.allocated_replicas
                );
            }
        }
        PoolCommands::Create {
            name,
            image,
            replicas,
            wait: _,
        } => {
            client.create_pool(&name, &image, replicas).await?;
            if !json_output {
                println!("pool {name} created.");
            }
        }
        PoolCommands::Scale { name, replicas } => {
            client.scale_pool(&name, replicas).await?;
            if !json_output {
                println!("pool {name} scaled to {replicas}.");
            }
        }
        PoolCommands::Rm { name, force } => {
            if !force {
                eprintln!("delete pool {name}? use --force");
                std::process::exit(1);
            }
            client.delete_pool(&name).await?;
            if !json_output {
                println!("pool {name} deleted.");
            }
        }
    }
    Ok(())
}

fn cmd_config(sub: ConfigCommands) -> anyhow::Result<()> {
    match sub {
        ConfigCommands::Show => {
            let cfg = config::Config::load();
            println!("gateway: {}", cfg.gateway_url);
        }
        ConfigCommands::Set { key, value } => {
            let mut cfg = config::Config::load();
            match key.as_str() {
                "gateway" | "gateway-url" | "gateway_url" => cfg.gateway_url = value,
                other => anyhow::bail!("unknown config key: {other}"),
            }
            cfg.save()?;
            println!("ok");
        }
    }
    Ok(())
}
