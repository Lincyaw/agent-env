fn main() {
    prost_build::Config::new()
        .out_dir("src/")
        .compile_protos(&["../../proto/executor_v2.proto"], &["../../proto/"])
        .expect("protobuf compilation failed");
}
