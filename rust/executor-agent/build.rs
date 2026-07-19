fn main() {
    prost_build::Config::new()
        .compile_protos(
            &["../../proto/executor.proto"],
            &["../../proto/"],
        )
        .expect("protobuf compilation failed");
}
