fn main() {
    prost_build::compile_protos(&["../../proto/executor_v2.proto"], &["../../proto/"])
        .expect("protobuf compilation failed");
}
