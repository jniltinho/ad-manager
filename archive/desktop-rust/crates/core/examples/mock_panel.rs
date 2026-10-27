//! A mock panel for the benchmark and manual runs: `cargo run -p adm-core --example mock_panel --features testkit --
//! [--users N] [--port P]`. It prints the URL and the certificate pin to put in desktop.toml, then one line per
//! request (`REQ <unix ms> <method> <path>`, read by bench/run.sh to know what an app asked for) until killed.

use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use adm_core::testkit::{MockPanel, Req, demo};

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().collect();
    let arg = |k: &str, d: u64| {
        args.iter().position(|a| a == k).and_then(|i| args.get(i + 1)).and_then(|v| v.parse().ok()).unwrap_or(d)
    };
    let users = arg("--users", 5000) as usize;
    let port = arg("--port", 0) as u16;
    let inner = demo(users);
    let route = Arc::new(move |r: &Req| {
        let answer = inner(r);
        let ms = SystemTime::now().duration_since(UNIX_EPOCH).map_or(0, |d| d.as_millis());
        println!("REQ {ms} {} {}", r.method, r.path);
        answer
    });
    let m = MockPanel::start(port, route).await?;
    println!("url = \"{}\"\nfingerprint = \"{}\"\nusers = {users}", m.url, m.pin);
    tokio::signal::ctrl_c().await?;
    Ok(())
}
