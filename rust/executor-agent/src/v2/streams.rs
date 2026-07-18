#![allow(dead_code)]

/// Data stream type tags for multi-stream QUIC.
/// Each data stream starts with a 5-byte header: [1B type][4B tag].
pub const STREAM_TYPE_STDOUT: u8 = 0x01;
pub const STREAM_TYPE_STDERR: u8 = 0x02;
pub const STREAM_TYPE_FILE_READ: u8 = 0x03;
pub const STREAM_TYPE_FILE_WRITE: u8 = 0x04;
pub const STREAM_TYPE_TUNNEL: u8 = 0x05;
pub const STREAM_TYPE_STDIN: u8 = 0x06;

/// Write the 5-byte data stream header.
pub async fn write_stream_header(
    stream: &mut iroh::endpoint::SendStream,
    stream_type: u8,
    tag: u32,
) -> Result<(), iroh::endpoint::WriteError> {
    let mut hdr = [0u8; 5];
    hdr[0] = stream_type;
    hdr[1..5].copy_from_slice(&tag.to_be_bytes());
    stream.write_all(&hdr).await?;
    Ok(())
}

/// Read the 5-byte data stream header, returning (type, tag).
pub async fn read_stream_header(
    stream: &mut iroh::endpoint::RecvStream,
) -> Result<(u8, u32), Box<dyn std::error::Error + Send + Sync>> {
    let mut hdr = [0u8; 5];
    stream.read_exact(&mut hdr).await?;
    let stream_type = hdr[0];
    let tag = u32::from_be_bytes([hdr[1], hdr[2], hdr[3], hdr[4]]);
    Ok((stream_type, tag))
}
