use criterion::{criterion_group, criterion_main, BenchmarkId, Criterion, Throughput};
use executor_agent::executor::agent::{MSG_TYPE_REQUEST, MSG_TYPE_RESPONSE, MSG_TYPE_EVENT};
use executor_agent::executor::proto;
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
        let agent = executor_agent::executor::agent::Agent::new(sp, ws, None);
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

fn send_pb(stream: &mut UnixStream, tag: u32, kind: proto::request::Kind) {
    let request = proto::Request {
        tag,
        kind: Some(kind),
    };
    let encoded = request.encode_to_vec();
    let len = encoded.len() as u32;
    stream.write_all(&[MSG_TYPE_REQUEST]).unwrap();
    stream.write_all(&len.to_be_bytes()).unwrap();
    stream.write_all(&encoded).unwrap();
    stream.flush().unwrap();
}

/// Read a typed message, returns (type_byte, raw_bytes).
fn recv_typed(stream: &mut UnixStream) -> (u8, Vec<u8>) {
    let mut type_buf = [0u8; 1];
    stream.read_exact(&mut type_buf).unwrap();
    let mut len_buf = [0u8; 4];
    stream.read_exact(&mut len_buf).unwrap();
    let len = u32::from_be_bytes(len_buf) as usize;
    let mut msg_buf = vec![0u8; len];
    stream.read_exact(&mut msg_buf).unwrap();
    (type_buf[0], msg_buf)
}

fn recv_response(stream: &mut UnixStream) -> proto::Response {
    let (typ, data) = recv_typed(stream);
    assert_eq!(typ, MSG_TYPE_RESPONSE);
    proto::Response::decode(&data[..]).unwrap()
}

fn bench_ping(c: &mut Criterion) {
    let ws = tempfile::tempdir().unwrap();
    let sock = start_agent(ws.path().to_str().unwrap());
    let mut stream = connect(&sock);

    c.bench_function("ping_roundtrip", |b| {
        b.iter(|| {
            send_pb(
                &mut stream,
                1,
                proto::request::Kind::Ping(proto::PingRequest {}),
            );
            recv_response(&mut stream);
        });
    });
}

fn bench_exec(c: &mut Criterion) {
    let ws = tempfile::tempdir().unwrap();
    let sock = start_agent(ws.path().to_str().unwrap());
    let mut stream = connect(&sock);
    let mut tag = 100u32;

    c.bench_function("exec_echo", |b| {
        b.iter(|| {
            tag += 1;
            send_pb(
                &mut stream,
                tag,
                proto::request::Kind::Spawn(proto::SpawnRequest {
                    command: vec!["echo".into(), "bench".into()],
                    ..Default::default()
                }),
            );
            loop {
                let (typ, data) = recv_typed(&mut stream);
                if typ == MSG_TYPE_EVENT {
                    let evt = proto::Event::decode(&data[..]).unwrap();
                    if matches!(&evt.kind, Some(proto::event::Kind::Exit(_))) {
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

    let mut group = c.benchmark_group("file_write");

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
                        i as u32,
                        proto::request::Kind::Write(proto::WriteRequest {
                            path: fname,
                            expected_sha256: String::new(),
                            size_hint: data.len() as i64,
                        }),
                    );
                    // Send raw data frame
                    let len = data.len() as u32;
                    stream.write_all(&len.to_be_bytes()).unwrap();
                    stream.write_all(data).unwrap();
                    // terminator
                    stream.write_all(&0u32.to_be_bytes()).unwrap();
                    stream.flush().unwrap();
                    recv_response(&mut stream);
                });
            },
        );
    }
    group.finish();
}

fn bench_file_read(c: &mut Criterion) {
    let ws = tempfile::tempdir().unwrap();
    let sock = start_agent(ws.path().to_str().unwrap());

    let mut group = c.benchmark_group("file_read");

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
                        1,
                        proto::request::Kind::Read(proto::ReadRequest {
                            path: fname.clone(),
                        }),
                    );
                    // Read ReadResponse
                    recv_response(&mut stream);
                    // Read raw data frames until zero terminator
                    loop {
                        let mut len_buf = [0u8; 4];
                        stream.read_exact(&mut len_buf).unwrap();
                        let len = u32::from_be_bytes(len_buf) as usize;
                        if len == 0 {
                            break;
                        }
                        let mut buf = vec![0u8; len];
                        stream.read_exact(&mut buf).unwrap();
                    }
                });
            },
        );
    }
    group.finish();
}

criterion_group!(benches, bench_ping, bench_exec, bench_file_write, bench_file_read);
criterion_main!(benches);
