//! Length-prefixed framing shared by every stream-based transport (TCP now,
//! TLS-over-TCP below) — a 4-byte big-endian length prefix followed by that
//! many payload bytes. Extracted so `Connection` impls don't each redefine
//! (and risk drifting on) `MAX_FRAME_LEN` or the wire format.

use crate::error::{NexError, Result};
use tokio::io::{AsyncRead, AsyncReadExt, AsyncWrite, AsyncWriteExt};

/// Guards against a corrupt/hostile length prefix causing an unbounded
/// allocation before we've even validated the frame.
pub const MAX_FRAME_LEN: u32 = 16 * 1024 * 1024; // 16 MiB

pub async fn write_frame<W: AsyncWrite + Unpin>(writer: &mut W, data: &[u8]) -> Result<()> {
    let len: u32 = data
        .len()
        .try_into()
        .map_err(|_| NexError::Transport("frame too large to send".to_string()))?;
    writer
        .write_all(&len.to_be_bytes())
        .await
        .map_err(|e| NexError::Transport(e.to_string()))?;
    writer
        .write_all(data)
        .await
        .map_err(|e| NexError::Transport(e.to_string()))?;
    Ok(())
}

pub async fn read_frame<R: AsyncRead + Unpin>(reader: &mut R) -> Result<Vec<u8>> {
    let mut len_buf = [0u8; 4];
    reader
        .read_exact(&mut len_buf)
        .await
        .map_err(|e| NexError::Transport(e.to_string()))?;
    let len = u32::from_be_bytes(len_buf);
    if len > MAX_FRAME_LEN {
        return Err(NexError::Transport(format!(
            "frame length {len} exceeds max {MAX_FRAME_LEN}"
        )));
    }

    let mut buf = vec![0u8; len as usize];
    reader
        .read_exact(&mut buf)
        .await
        .map_err(|e| NexError::Transport(e.to_string()))?;
    Ok(buf)
}
