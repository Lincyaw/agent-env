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
        /// Experiment ID
        #[arg(short, long)]
        experiment: Option<String>,
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
        /// Key (e.g. "gateway", "api-key")
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
    let api_key = if cfg.api_key.is_empty() { None } else { Some(cfg.api_key.clone()) };
    let json_output = cli.format == "json";
    let client = gateway::Client::with_api_key(&gateway_url, api_key);

    let result = match cli.command {
        Commands::Run {
            image,
            command,
            experiment,
        } => cmd_run(&client, &gateway_url, &image, &command, experiment.as_deref(), json_output).await,

        Commands::Create {
            image,
            experiment,
        } => cmd_create(&client, &gateway_url, &image, experiment.as_deref(), json_output).await,

        Commands::Attach { session } => cmd_attach(&client, &gateway_url, &session).await,

        Commands::Exec { session, command } => {
            cmd_exec(&client, &gateway_url, &session, &command, json_output).await
        }

        Commands::Tunnel {
            session,
            remote_port,
            local,
            remote_host,
        } => {
            cmd_tunnel(
                &client,
                &remote_host,
                remote_port,
                local.unwrap_or(remote_port),
                &session,
            )
            .await
        }

        Commands::Cp { src, dst } => cmd_cp(&client, &gateway_url, &src, &dst, json_output).await,

        Commands::Ps { all, experiment } => {
            cmd_ps(&client, all, experiment.as_deref(), json_output).await
        }

        Commands::Logs {
            session,
            follow,
            tail,
        } => cmd_logs(&client, &session, follow, tail).await,

        Commands::Kill { sessions } => cmd_kill(&client, &sessions, json_output).await,

        Commands::Status => cmd_status(&client, &gateway_url, json_output).await,

        Commands::Exp(sub) => cmd_exp(&client, sub, json_output).await,
        Commands::Pool(sub) => cmd_pool(&client, sub, json_output).await,
        Commands::Config(sub) => cmd_config(sub),
    };

    if let Err(e) = result {
        eprintln!("error: {e}");
        std::process::exit(1);
    }
}

async fn cmd_run(
    client: &gateway::Client,
    gw: &str,
    image: &str,
    command: &[String],
    experiment: Option<&str>,
    json_output: bool,
) -> anyhow::Result<()> {
    let auto_exp = experiment.is_none();
    let exp_id = experiment
        .map(String::from)
        .unwrap_or_else(|| format!("run-{}", std::process::id()));

    let session = client.create_managed_session(image, &exp_id).await?;
    let sid = &session.id;

    let transport = transport::connect(gw, &session, client).await;

    let result = if command.is_empty() {
        transport.shell(sid).await
    } else {
        transport.exec(sid, command).await
    };

    let _ = client.delete_session(sid).await;
    if auto_exp {
        let _ = client.delete_experiment(&exp_id).await;
    }

    let output = result?;
    print_and_exit(output, json_output)
}

fn print_and_exit(output: gateway::ExecOutput, json_output: bool) -> anyhow::Result<()> {
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
    client: &gateway::Client,
    gw: &str,
    image: &str,
    experiment: Option<&str>,
    json_output: bool,
) -> anyhow::Result<()> {
    let _ = gw;
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

async fn cmd_attach(client: &gateway::Client, gw: &str, session: &str) -> anyhow::Result<()> {
    let info = client.get_session(session).await?;
    let transport = transport::connect(gw, &info, client).await;
    let output = transport.shell(&info.id).await?;
    if output.exit_code != 0 {
        std::process::exit(output.exit_code);
    }
    Ok(())
}

async fn cmd_exec(
    client: &gateway::Client,
    gw: &str,
    session: &str,
    command: &[String],
    json_output: bool,
) -> anyhow::Result<()> {
    if command.is_empty() {
        anyhow::bail!("no command specified");
    }
    let info = client.get_session(session).await?;
    let transport = transport::connect(gw, &info, client).await;
    let output = transport.exec(&info.id, command).await?;
    print_and_exit(output, json_output)
}

async fn cmd_tunnel(
    client: &gateway::Client,
    remote_host: &str,
    remote_port: u16,
    local_port: u16,
    session: &str,
) -> anyhow::Result<()> {
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
    client: &gateway::Client,
    gw: &str,
    src: &str,
    dst: &str,
    json_output: bool,
) -> anyhow::Result<()> {
    let (session_id, remote_path, local_path, is_upload) = parse_cp_args(src, dst)?;
    let info = client.get_session(&session_id).await?;
    let transport = transport::connect(gw, &info, client).await;

    if is_upload {
        let data = tokio::fs::read(&local_path).await?;
        let result = transport.upload(&info.id, &remote_path, data).await?;
        if json_output {
            println!("{}", serde_json::to_string_pretty(&result)?);
        } else {
            eprintln!("uploaded {} ({} bytes)", remote_path, result.bytes_written);
        }
    } else {
        let data = transport.download(&info.id, &remote_path).await?;
        tokio::fs::write(&local_path, &data).await?;
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
    client: &gateway::Client,
    _all: bool,
    experiment: Option<&str>,
    json_output: bool,
) -> anyhow::Result<()> {
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
    client: &gateway::Client,
    session: &str,
    _follow: bool,
    _tail: usize,
) -> anyhow::Result<()> {
    let logs = client.get_logs(session).await?;
    print!("{logs}");
    Ok(())
}

async fn cmd_kill(client: &gateway::Client, sessions: &[String], json_output: bool) -> anyhow::Result<()> {
    let futs: Vec<_> = sessions
        .iter()
        .map(|sid| {
            async move { (sid.clone(), client.delete_session(sid).await) }
        })
        .collect();
    let mut failed = false;
    for (sid, result) in futures::future::join_all(futs).await {
        match result {
            Ok(()) if !json_output => println!("killed {sid}"),
            Err(e) => {
                eprintln!("failed to kill {sid}: {e}");
                failed = true;
            }
            _ => {}
        }
    }
    if failed {
        anyhow::bail!("some sessions could not be killed");
    }
    Ok(())
}

async fn cmd_status(client: &gateway::Client, gw: &str, json_output: bool) -> anyhow::Result<()> {
    let (health, summary) = tokio::join!(client.health(), client.summary());

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

async fn cmd_exp(client: &gateway::Client, sub: ExpCommands, json_output: bool) -> anyhow::Result<()> {
    match sub {
        ExpCommands::Create {
            id,
            image,
            sessions: count,
            profile: _,
        } => {
            use futures::stream::{self, StreamExt};
            let mut created = Vec::new();
            let mut stream = stream::iter(0..count)
                .map(|_| client.create_managed_session(&image, &id))
                .buffer_unordered(8);
            while let Some(r) = stream.next().await {
                match r {
                    Ok(s) => created.push(s),
                    Err(e) => eprintln!("session creation failed: {e}"),
                }
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

async fn cmd_pool(client: &gateway::Client, sub: PoolCommands, json_output: bool) -> anyhow::Result<()> {
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
            println!("gateway:  {}", cfg.gateway_url);
            println!("api-key:  {}", if cfg.api_key.is_empty() { "(not set)" } else { "(set)" });
        }
        ConfigCommands::Set { key, value } => {
            let mut cfg = config::Config::load();
            match key.as_str() {
                "gateway" | "gateway-url" | "gateway_url" => cfg.gateway_url = value,
                "api-key" | "api_key" => cfg.api_key = value,
                other => anyhow::bail!("unknown config key: {other}"),
            }
            cfg.save()?;
            println!("ok");
        }
    }
    Ok(())
}
