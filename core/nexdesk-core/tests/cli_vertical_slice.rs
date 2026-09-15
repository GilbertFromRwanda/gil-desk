//! Process-level Gate G1 test (planner tasks R-26/R-27/R-28): spawns the
//! actual compiled `host` and `client` binaries as separate OS processes
//! (not library calls) and drives them through connect, secure handshake,
//! and reconnect-after-failure — proving the *artifacts*, not just the
//! library code they're built from, satisfy Gate G1.

use std::time::Duration;
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio::process::{Child, Command};
use tokio::time::timeout;

fn unique_temp_dir() -> std::path::PathBuf {
    let dir = std::env::temp_dir().join(format!(
        "nexdesk-cli-test-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos()
    ));
    std::fs::create_dir_all(&dir).unwrap();
    dir
}

/// Reads lines from `child`'s stdout until one contains `needle`, or the
/// timeout elapses. Returns the matching line.
async fn wait_for_line(reader: &mut BufReader<tokio::process::ChildStdout>, needle: &str) -> String {
    timeout(Duration::from_secs(10), async {
        let mut line = String::new();
        loop {
            line.clear();
            let n = reader.read_line(&mut line).await.unwrap();
            assert!(n > 0, "process exited before printing {needle:?}");
            if line.contains(needle) {
                return line.trim().to_string();
            }
        }
    })
    .await
    .unwrap_or_else(|_| panic!("timed out waiting for line containing {needle:?}"))
}

fn spawn_with_stdout(cmd: &mut Command) -> (Child, BufReader<tokio::process::ChildStdout>) {
    let mut child = cmd
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()
        .unwrap();
    let stdout = child.stdout.take().unwrap();
    (child, BufReader::new(stdout))
}

#[tokio::test]
async fn cli_host_and_client_connect_and_reconnect_after_failure() {
    let dir = unique_temp_dir();
    let cert_path = dir.join("host_cert.der");

    // --- 1/2: start the host, discover its bound address, connect ---
    let (mut host, mut host_out) = spawn_with_stdout(
        Command::new(env!("CARGO_BIN_EXE_host"))
            .arg("--bind")
            .arg("127.0.0.1:0")
            .arg("--device-id")
            .arg("test-host")
            .arg("--cert-out")
            .arg(&cert_path),
    );

    let listening_line = wait_for_line(&mut host_out, "LISTENING").await;
    let addr = listening_line
        .strip_prefix("LISTENING ")
        .expect("LISTENING line must carry the bound address")
        .to_string();

    let (mut client, mut client_out) = spawn_with_stdout(
        Command::new(env!("CARGO_BIN_EXE_client"))
            .arg("--server")
            .arg(&addr)
            .arg("--cert")
            .arg(&cert_path)
            .arg("--device-id")
            .arg("test-client"),
    );

    // --- 2/3: secure session established, protocol messages (heartbeats)
    // flow both ways ---
    let client_established = wait_for_line(&mut client_out, "ESTABLISHED").await;
    assert!(client_established.contains("peer=test-host"));
    let host_established = wait_for_line(&mut host_out, "ESTABLISHED").await;
    assert!(host_established.contains("peer=test-client"));

    wait_for_line(&mut client_out, "RECV PING").await;
    wait_for_line(&mut host_out, "RECV PING").await;

    // --- 4: reconnect after interruption — kill the host out from under
    // the client, confirm the client notices and starts retrying, then
    // bring the host back up on the same address+identity and confirm the
    // client re-establishes ---
    host.kill().await.unwrap();
    wait_for_line(&mut client_out, "RECONNECTING").await;

    let (mut host2, mut host2_out) = spawn_with_stdout(
        Command::new(env!("CARGO_BIN_EXE_host"))
            .arg("--bind")
            .arg(&addr)
            .arg("--device-id")
            .arg("test-host")
            .arg("--cert-out")
            .arg(&cert_path),
    );
    wait_for_line(&mut host2_out, "LISTENING").await;

    let reconnected = wait_for_line(&mut client_out, "ESTABLISHED").await;
    assert!(reconnected.contains("peer=test-host"));

    // --- 5: shut down cleanly ---
    client.kill().await.unwrap();
    host2.kill().await.unwrap();
    let _ = std::fs::remove_dir_all(&dir);
}
