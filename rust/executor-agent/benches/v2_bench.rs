use base64::Engine;
use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use std::io::{BufRead, BufReader, Write};
use std::os::unix::net::UnixStream;
use std::thread;
use std::time::Duration;

fn start_agent(workspace: &str) -> String {
    let dir = tempfile::tempdir().unwrap();
    let sock_path = dir.path().join("bench.sock").to_str().unwrap().to_string();

    let sp = sock_path.clone();
    let ws = workspace.to_string();
    thread::spawn(move || {
        let agent = executor_agent::v2::agent::AgentV2::new(sp, ws);
        let (_tx, rx) = tokio::sync::watch::channel(false);
        agent.run(rx).ok();
    });

    for _ in 0..200 {
        if std::path::Path::new(&sock_path).exists() {
            break;
        }
        thread::sleep(Duration::from_millis(10));
    }

    std::mem::forget(dir);
    sock_path
}

fn connect(sock: &str) -> (UnixStream, BufReader<UnixStream>) {
    let stream = UnixStream::connect(sock).unwrap();
    let reader = BufReader::new(stream.try_clone().unwrap());
    (stream, reader)
}

fn send(stream: &mut UnixStream, msg: &str) {
    stream.write_all(msg.as_bytes()).unwrap();
    stream.write_all(b"\n").unwrap();
    stream.flush().unwrap();
}

fn recv(reader: &mut BufReader<UnixStream>) -> String {
    let mut line = String::new();
    reader.read_line(&mut line).unwrap();
    line
}

fn bench_ping(c: &mut Criterion) {
    let ws = tempfile::tempdir().unwrap();
    let sock = start_agent(ws.path().to_str().unwrap());
    let (mut stream, mut reader) = connect(&sock);

    c.bench_function("v2_ping_roundtrip", |b| {
        b.iter(|| {
            send(&mut stream, r#"{"id":"b","method":"ping"}"#);
            recv(&mut reader);
        });
    });
}

fn bench_exec(c: &mut Criterion) {
    let ws = tempfile::tempdir().unwrap();
    let sock = start_agent(ws.path().to_str().unwrap());
    let (mut stream, mut reader) = connect(&sock);

    c.bench_function("v2_exec_echo", |b| {
        b.iter(|| {
            send(
                &mut stream,
                r#"{"id":"b","method":"spawn","params":{"cmd":["echo","bench"]}}"#,
            );
            // Read spawn result + stdout + exit
            loop {
                let line = recv(&mut reader);
                let v: serde_json::Value = serde_json::from_str(line.trim()).unwrap();
                if v.get("event").is_some() && v["event"] == "exit" {
                    break;
                }
            }
        });
    });
}

fn bench_file_write(c: &mut Criterion) {
    let ws = tempfile::tempdir().unwrap();
    let sock = start_agent(ws.path().to_str().unwrap());

    let mut group = c.benchmark_group("v2_file_write");

    for size_kb in [1, 64, 256, 1024] {
        let data = vec![0x55u8; size_kb * 1024];
        let b64 = base64::engine::general_purpose::STANDARD.encode(&data);

        group.throughput(Throughput::Bytes((size_kb * 1024) as u64));
        group.bench_with_input(BenchmarkId::from_parameter(format!("{size_kb}KB")), &b64, |b, b64| {
            let (mut stream, mut reader) = connect(&sock);
            let mut i = 0u64;
            b.iter(|| {
                i += 1;
                let fname = format!("bench_{i}.bin");
                send(
                    &mut stream,
                    &format!(r#"{{"id":"w","method":"write_file","params":{{"path":"{fname}"}}}}"#),
                );
                send(
                    &mut stream,
                    &format!(r#"{{"id":"w","method":"file_chunk","params":{{"content":"{b64}"}}}}"#),
                );
                send(&mut stream, r#"{"id":"w","method":"file_done"}"#);
                recv(&mut reader);
            });
        });
    }
    group.finish();
}

fn bench_file_read(c: &mut Criterion) {
    let ws = tempfile::tempdir().unwrap();
    let sock = start_agent(ws.path().to_str().unwrap());

    let mut group = c.benchmark_group("v2_file_read");

    for size_kb in [1, 64, 256, 1024] {
        let data = vec![0xAAu8; size_kb * 1024];
        let fname = format!("read_{size_kb}kb.bin");
        std::fs::write(ws.path().join(&fname), &data).unwrap();

        group.throughput(Throughput::Bytes((size_kb * 1024) as u64));
        group.bench_with_input(BenchmarkId::from_parameter(format!("{size_kb}KB")), &fname, |b, fname| {
            let (mut stream, mut reader) = connect(&sock);
            b.iter(|| {
                send(
                    &mut stream,
                    &format!(r#"{{"id":"r","method":"read_file","params":{{"path":"{fname}"}}}}"#),
                );
                loop {
                    let line = recv(&mut reader);
                    let v: serde_json::Value = serde_json::from_str(line.trim()).unwrap();
                    if v.get("result").is_some() {
                        break;
                    }
                }
            });
        });
    }
    group.finish();
}

fn bench_stat(c: &mut Criterion) {
    let ws = tempfile::tempdir().unwrap();
    std::fs::write(ws.path().join("s.txt"), "x").unwrap();
    let sock = start_agent(ws.path().to_str().unwrap());
    let (mut stream, mut reader) = connect(&sock);

    c.bench_function("v2_stat", |b| {
        b.iter(|| {
            send(
                &mut stream,
                r#"{"id":"s","method":"stat","params":{"path":"s.txt"}}"#,
            );
            recv(&mut reader);
        });
    });
}

criterion_group!(benches, bench_ping, bench_exec, bench_file_write, bench_file_read, bench_stat);
criterion_main!(benches);
