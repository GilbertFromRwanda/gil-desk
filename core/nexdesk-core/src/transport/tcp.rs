//! TCP transport (planner task R-08): length-prefixed framing over
//! `tokio::net::TcpStream` — a 4-byte big-endian length prefix followed by
//! that many payload bytes.

use crate::error::{NexError, Result};
use crate::transport::Connection;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpListener as TokioTcpListener, TcpStream};

/// Guards against a corrupt/hostile length prefix causing an unbounded
/// allocation before we've even validated the frame.
const MAX_FRAME_LEN: u32 = 16 * 1024 * 1024; // 16 MiB

pub struct TcpConnection {
    stream: TcpStream,
}

impl TcpConnection {
    pub async fn connect(addr: &str) -> Result<Self> {
        let stream = TcpStream::connect(addr)
            .await
            .map_err(|e| NexError::Transport(e.to_string()))?;
        Ok(Self { stream })
    }

    fn from_stream(stream: TcpStream) -> Self {
        Self { stream }
    }
}

impl Connection for TcpConnection {
    async fn send(&mut self, data: &[u8]) -> Result<()> {
        let len: u32 = data
            .len()
            .try_into()
            .map_err(|_| NexError::Transport("frame too large to send".to_string()))?;
        self.stream
            .write_all(&len.to_be_bytes())
            .await
            .map_err(|e| NexError::Transport(e.to_string()))?;
        self.stream
            .write_all(data)
            .await
            .map_err(|e| NexError::Transport(e.to_string()))?;
        Ok(())
    }

    async fn recv(&mut self) -> Result<Vec<u8>> {
        let mut len_buf = [0u8; 4];
        self.stream
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
        self.stream
            .read_exact(&mut buf)
            .await
            .map_err(|e| NexError::Transport(e.to_string()))?;
        Ok(buf)
    }
}

pub struct TcpListener {
    listener: TokioTcpListener,
}

impl TcpListener {
    pub async fn bind(addr: &str) -> Result<Self> {
        let listener = TokioTcpListener::bind(addr)
            .await
            .map_err(|e| NexError::Transport(e.to_string()))?;
        Ok(Self { listener })
    }

    pub fn local_addr(&self) -> Result<std::net::SocketAddr> {
        self.listener
            .local_addr()
            .map_err(|e| NexError::Transport(e.to_string()))
    }

    pub async fn accept(&self) -> Result<TcpConnection> {
        let (stream, _) = self
            .listener
            .accept()
            .await
            .map_err(|e| NexError::Transport(e.to_string()))?;
        Ok(TcpConnection::from_stream(stream))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    #[tokio::test]
    async fn round_trips_a_message_over_a_real_socket() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        let server = tokio::spawn(async move {
            let mut conn = listener.accept().await.unwrap();
            let msg = conn.recv().await.unwrap();
            conn.send(&msg).await.unwrap(); // echo
        });

        let mut client = TcpConnection::connect(&addr.to_string()).await.unwrap();
        client.send(b"hello nexdesk").await.unwrap();
        let echoed = client.recv().await.unwrap();

        server.await.unwrap();
        assert_eq!(echoed, b"hello nexdesk");
    }

    #[tokio::test]
    async fn rejects_frame_larger_than_max_without_allocating_it() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        let client_task = tokio::spawn(async move {
            let mut stream = TcpStream::connect(addr.to_string()).await.unwrap();
            let bogus_len: u32 = MAX_FRAME_LEN + 1;
            stream.write_all(&bogus_len.to_be_bytes()).await.unwrap();
            // Keep the socket open until the server has read the header.
            tokio::time::sleep(Duration::from_millis(100)).await;
        });

        let mut conn = listener.accept().await.unwrap();
        let result = conn.recv().await;
        assert!(matches!(result, Err(NexError::Transport(_))));

        client_task.await.unwrap();
    }

    #[tokio::test]
    async fn recv_errors_cleanly_on_peer_disconnect() {
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();

        let client_task = tokio::spawn(async move {
            let _stream = TcpStream::connect(addr.to_string()).await.unwrap();
            // Drop immediately: closes the socket without sending anything.
        });

        let mut conn = listener.accept().await.unwrap();
        client_task.await.unwrap();
        let result = conn.recv().await;
        assert!(result.is_err());
    }
}
