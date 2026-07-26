fn main() {
    println!("cargo:rerun-if-changed=../../proto/executor_v2.proto");
    prost_build::Config::new()
        .compile_protos(
            &["../../proto/executor_v2.proto"],
            &["../../proto/"],
        )
        .expect("protobuf compilation failed");
}
