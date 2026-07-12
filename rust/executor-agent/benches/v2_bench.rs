use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use executor_agent::v2::proto;
use prost::Message;
use std::io::{Read, Write};
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

fn connect(sock: &str) -> UnixStream {
    UnixStream::connect(sock).unwrap()
}

fn send_pb(stream: &mut UnixStream, id: &str, method: proto::request::Method) {
    let envelope = proto::Envelope {
        payload: Some(proto::envelope::Payload::Request(proto::Request {
            id: id.to_string(),
            method: Some(method),
        })),
    };
    let encoded = envelope.encode_to_vec();
    let len = encoded.len() as u32;
    stream.write_all(&len.to_be_bytes()).unwrap();
    stream.write_all(&encoded).unwrap();
    stream.flush().unwrap();
}

fn recv_pb(stream: &mut UnixStream) -> proto::Envelope {
    let mut len_buf = [0u8; 4];
    stream.read_exact(&mut len_buf).unwrap();
    let len = u32::from_be_bytes(len_buf) as usize;
    let mut msg_buf = vec![0u8; len];
    stream.read_exact(&mut msg_buf).unwrap();
    proto::Envelope::decode(&msg_buf[..]).unwrap()
}

fn bench_ping(c: &mut Criterion) {
    let ws = tempfile::tempdir().unwrap();
    let sock = start_agent(ws.path().to_str().unwrap());
    let mut stream = connect(&sock);

    c.bench_function("v2_ping_roundtrip", |b| {
        b.iter(|| {
            send_pb(
                &mut stream,
                "b",
                proto::request::Method::Ping(proto::PingRequest {}),
            );
            recv_pb(&mut stream);
        });
    });
}

fn bench_exec(c: &mut Criterion) {
    let ws = tempfile::tempdir().unwrap();
    let sock = start_agent(ws.path().to_str().unwrap());
    let mut stream = connect(&sock);

    c.bench_function("v2_exec_echo", |b| {
        b.iter(|| {
            send_pb(
                &mut stream,
                "b",
                proto::request::Method::Spawn(proto::SpawnRequest {
                    cmd: vec!["echo".into(), "bench".into()],
                    ..Default::default()
                }),
            );
            loop {
                let env = recv_pb(&mut stream);
                if let Some(proto::envelope::Payload::Event(evt)) = &env.payload {
                    if matches!(&evt.event, Some(proto::event::Event::Exit(_))) {
                        break;
                    }
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

        group.throughput(Throughput::Bytes((size_kb * 1024) as u64));
        group.bench_with_input(
            BenchmarkId::from_parameter(format!("{size_kb}KB")),
            &data,
            |b, data| {
                let mut stream = connect(&sock);
                let mut i = 0u64;
                b.iter(|| {
                    i += 1;
                    let fname = format!("bench_{i}.bin");
                    send_pb(
                        &mut stream,
                        "w",
                        proto::request::Method::WriteFile(proto::WriteFileRequest {
                            path: fname,
                            expected_sha256: String::new(),
                            stream_tag: 0,
                        }),
                    );
                    send_pb(
                        &mut stream,
                        "w",
                        proto::request::Method::FileChunk(proto::FileChunkData {
                            content: data.clone(),
                        }),
                    );
                    send_pb(
                        &mut stream,
                        "w",
                        proto::request::Method::FileDone(proto::FileDoneRequest {}),
                    );
                    recv_pb(&mut stream);
                });
            },
        );
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
        group.bench_with_input(
            BenchmarkId::from_parameter(format!("{size_kb}KB")),
            &fname,
            |b, fname| {
                let mut stream = connect(&sock);
                b.iter(|| {
                    send_pb(
                        &mut stream,
                        "r",
                        proto::request::Method::ReadFile(proto::ReadFileRequest {
                            path: fname.clone(),
                        }),
                    );
                    loop {
                        let env = recv_pb(&mut stream);
                        if let Some(proto::envelope::Payload::Response(resp)) = &env.payload {
                            if matches!(
                                &resp.result,
                                Some(proto::response::Result::FileDone(_))
                            ) {
                                break;
                            }
                        }
                    }
                });
            },
        );
    }
    group.finish();
}

fn bench_stat(c: &mut Criterion) {
    let ws = tempfile::tempdir().unwrap();
    std::fs::write(ws.path().join("s.txt"), "x").unwrap();
    let sock = start_agent(ws.path().to_str().unwrap());
    let mut stream = connect(&sock);

    c.bench_function("v2_stat", |b| {
        b.iter(|| {
            send_pb(
                &mut stream,
                "s",
                proto::request::Method::Stat(proto::StatRequest {
                    path: "s.txt".into(),
                }),
            );
            recv_pb(&mut stream);
        });
    });
}

criterion_group!(benches, bench_ping, bench_exec, bench_file_write, bench_file_read, bench_stat);
criterion_main!(benches);
