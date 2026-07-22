fn main() {
    println!("cargo:rerun-if-changed=../../proto/executor.proto");
    prost_build::Config::new()
        .compile_protos(
            &["../../proto/executor.proto"],
            &["../../proto/"],
        )
        .expect("protobuf compilation failed");
}
