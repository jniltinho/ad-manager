//! An in-process HTTPS mock of the panel (feature `testkit`): a self-signed certificate, a route function and the
//! requests it received. [`demo`] answers every endpoint the app uses with the fictitious `domain.local` of Example
//! Company Ltd — the data of the UI tests, the screenshots and the benchmark (`examples/mock_panel.rs`).

use std::sync::{Arc, Mutex, PoisonError};

use serde_json::{Value, json};
use tokio::io::{AsyncReadExt, AsyncWriteExt};

use crate::tls::{cert_pin, provider};

/// A request the mock received.
#[derive(Debug, Clone, PartialEq)]
pub struct Req {
    pub method: String,
    pub path: String,
    pub body: String,
}

impl Req {
    pub fn json(&self) -> Value {
        serde_json::from_str(&self.body).unwrap_or(Value::Null)
    }
}

/// The answer to a request: status and JSON body.
pub type Route = dyn Fn(&Req) -> (u16, String) + Send + Sync;

pub struct MockPanel {
    pub url: String,
    pub pin: String,
    log: Arc<Mutex<Vec<Req>>>,
}

#[derive(Debug, thiserror::Error)]
pub enum MockError {
    #[error("{0}")]
    Cert(#[from] rcgen::Error),
    #[error("{0}")]
    Tls(#[from] rustls::Error),
    #[error("{0}")]
    Io(#[from] std::io::Error),
}

impl MockPanel {
    /// Starts the mock on 127.0.0.1 (`port` 0: any free one) answering with `route`.
    pub async fn start(port: u16, route: Arc<Route>) -> Result<Self, MockError> {
        Self::start_at("localhost", port, route).await
    }

    /// The mock reached as `host` (a name that resolves to 127.0.0.1, e.g. panel.domain.local in /etc/hosts).
    pub async fn start_at(host: &str, port: u16, route: Arc<Route>) -> Result<Self, MockError> {
        let k = rcgen::generate_simple_self_signed(vec![host.into(), "localhost".into(), "127.0.0.1".into()])?;
        let der = k.cert.der().clone();
        let pin = cert_pin(&der);
        let key = rustls::pki_types::PrivateKeyDer::Pkcs8(k.signing_key.serialize_der().into());
        let cfg = rustls::ServerConfig::builder_with_provider(provider())
            .with_safe_default_protocol_versions()?
            .with_no_client_auth()
            .with_single_cert(vec![der], key)?;
        let acceptor = tokio_rustls::TlsAcceptor::from(Arc::new(cfg));
        let l = tokio::net::TcpListener::bind(("127.0.0.1", port)).await?;
        let url = format!("https://{host}:{}", l.local_addr()?.port());
        let log: Arc<Mutex<Vec<Req>>> = Arc::default();
        let log2 = log.clone();
        tokio::spawn(async move {
            while let Ok((s, _)) = l.accept().await {
                let (a, route, log) = (acceptor.clone(), route.clone(), log2.clone());
                tokio::spawn(async move {
                    let Ok(mut t) = a.accept(s).await else { return };
                    let Some(req) = read_request(&mut t).await else { return };
                    log.lock().unwrap_or_else(PoisonError::into_inner).push(req.clone());
                    let (code, body) = route(&req);
                    let cookie = if req.path == "/api/login" && code == 200 {
                        "Set-Cookie: session=demo; Path=/; HttpOnly\r\n"
                    } else {
                        ""
                    };
                    let resp = format!(
                        "HTTP/1.1 {code} X\r\nContent-Type: application/json\r\n{cookie}Content-Length: {}\r\nConnection: close\r\n\r\n{body}",
                        body.len()
                    );
                    let _ = t.write_all(resp.as_bytes()).await;
                    let _ = t.shutdown().await;
                });
            }
        });
        Ok(Self { url, pin, log })
    }

    /// The requests received so far.
    pub fn requests(&self) -> Vec<Req> {
        self.log.lock().unwrap_or_else(PoisonError::into_inner).clone()
    }

    /// The requests of `method` to paths starting with `prefix`.
    pub fn sent(&self, method: &str, prefix: &str) -> Vec<Req> {
        self.requests().into_iter().filter(|r| r.method == method && r.path.starts_with(prefix)).collect()
    }
}

async fn read_request<S: AsyncReadExt + Unpin>(s: &mut S) -> Option<Req> {
    let mut buf = Vec::new();
    let mut chunk = [0u8; 8192];
    let head_end = loop {
        let n = s.read(&mut chunk).await.ok()?;
        if n == 0 {
            return None;
        }
        buf.extend_from_slice(&chunk[..n]);
        if let Some(i) = buf.windows(4).position(|w| w == b"\r\n\r\n") {
            break i + 4;
        }
        if buf.len() > 1 << 20 {
            return None;
        }
    };
    let head = String::from_utf8_lossy(&buf[..head_end]).to_string();
    let mut lines = head.lines();
    let mut first = lines.next()?.split_whitespace();
    let (method, path) = (first.next()?.to_string(), first.next()?.to_string());
    let len: usize = lines
        .filter_map(|l| l.split_once(':'))
        .find(|(k, _)| k.eq_ignore_ascii_case("content-length"))
        .and_then(|(_, v)| v.trim().parse().ok())
        .unwrap_or(0);
    while buf.len() < head_end + len {
        let n = s.read(&mut chunk).await.ok()?;
        if n == 0 {
            break;
        }
        buf.extend_from_slice(&chunk[..n]);
    }
    let body = String::from_utf8_lossy(&buf[head_end..buf.len().min(head_end + len)]).to_string();
    Some(Req { method, path, body })
}

const FIRST: [&str; 16] = [
    "Ana", "Bruno", "Carla", "Daniel", "Elisa", "Fabio", "Gabriela", "Hugo", "Isabel", "Joao", "Karina", "Lucas",
    "Marina", "Nelson", "Olivia", "Paulo",
];
const LAST: [&str; 8] = ["Silva", "Souza", "Costa", "Lima", "Rocha", "Alves", "Dias", "Melo"];
const DEPTS: [&str; 6] = ["Finance", "Sales", "IT", "HR", "Legal", "Operations"];

/// `n` users of domain.local (the first ones built-in), sorted by login.
pub fn demo_users(n: usize) -> Vec<Value> {
    let mut v = vec![
        json!({"sam":"Administrator","name":"Administrator","dn":"CN=Administrator,CN=Users,DC=domain,DC=local","enabled":true,"locked":false,"protected":true,"last_logon":"2026-10-22T09:12:00Z","sid":"S-1-5-21-1-500"}),
        json!({"sam":"Guest","name":"Guest","dn":"CN=Guest,CN=Users,DC=domain,DC=local","enabled":false,"locked":false,"protected":true,"sid":"S-1-5-21-1-501"}),
        json!({"sam":"krbtgt","name":"krbtgt","dn":"CN=krbtgt,CN=Users,DC=domain,DC=local","enabled":false,"locked":false,"protected":true,"sid":"S-1-5-21-1-502"}),
    ];
    for i in 0..n.saturating_sub(3) {
        let (f, l) = (FIRST[i % FIRST.len()], LAST[(i / FIRST.len()) % LAST.len()]);
        let sam = format!(
            "{}.{}{}",
            f.to_lowercase(),
            l.to_lowercase(),
            if i >= 128 { format!("{i}") } else { String::new() }
        );
        let dept = DEPTS[i % DEPTS.len()];
        let logon = if i % 11 == 0 {
            Value::Null
        } else {
            json!(format!("2026-{:02}-{:02}T08:30:00Z", 10 - (i % 7 == 0) as usize * 5, 1 + i % 20))
        };
        v.push(json!({
            "sam": sam, "name": format!("{f} {l}"), "mail": format!("{sam}@domain.local"), "department": dept,
            "dn": format!("CN={f} {l},OU={dept},OU=Example,DC=domain,DC=local"), "enabled": i % 17 != 5, "locked": i % 23 == 3,
            "protected": false, "last_logon": logon, "sid": format!("S-1-5-21-1-{}", 1100 + i), "upn": format!("{sam}@domain.local"),
            "title": "Analyst", "groups": ["Domain Users", dept],
        }));
    }
    v.sort_by_key(|u| u["sam"].as_str().unwrap_or_default().to_lowercase());
    v
}

/// A route answering the whole API with domain.local data and `users` users; writes answer `{}` (or a task).
pub fn demo(users: usize) -> Arc<Route> {
    let list = demo_users(users);
    let list_json = Value::Array(list.clone()).to_string();
    Arc::new(move |r: &Req| {
        let p = r.path.split('?').next().unwrap_or_default();
        let ok = |v: Value| (200, v.to_string());
        if r.method != "GET" {
            return match p {
                "/api/login" => ok(json!({"ok": true})),
                "/api/agents/1/tasks" => ok(json!({"id": "t1", "state": "pending"})),
                "/api/users/bruno.silva/password-link" => {
                    ok(json!({"token": "Zq3xY7", "expires": "2026-10-24T10:00:00Z"}))
                }
                _ => ok(json!({})),
            };
        }
        match p {
            "/api/health" => ok(json!({"domain":"domain.local","netbios":"DOMAIN","version":"0.22.0","ldap":"up",
                "brand":{"theme":"classic-blue","flat":true,"company":"Example Company Ltd","lang":"en","logo":false}})),
            "/api/i18n" => ok(json!({"messages": {}, "patterns": []})),
            "/api/me" => {
                ok(json!({"user":"administrator","domain":"domain.local","netbios":"DOMAIN","version":"0.22.0",
                "brand":{"theme":"classic-blue","flat":true,"company":"Example Company Ltd"},
                "settings":{"lang":"en","inactive_days":90,"password_length":14,"default_ou":""}}))
            }
            "/api/summary" => {
                let c = |f: &dyn Fn(&Value) -> bool| list.iter().filter(|u| f(u)).count();
                ok(json!({"users":{"enabled": c(&|u| u["enabled"] == true), "disabled": c(&|u| u["enabled"] == false),
                    "locked": c(&|u| u["locked"] == true)}, "groups": 42, "computers": 18,
                    "policy":{"complexity": true, "min_length": 7}}))
            }
            "/api/users" => (200, list_json.clone()),
            "/api/groups" => ok(json!([
                {"name":"Domain Admins","type":"security","scope":"global","protected":true,"member_count":2,"description":"Designated administrators of the domain"},
                {"name":"Domain Users","type":"security","scope":"global","protected":true,"member_count":users,"description":"All domain users"},
                {"name":"Finance","type":"security","scope":"global","protected":false,"member_count":12,"description":"Finance team"},
                {"name":"Sales","type":"security","scope":"global","protected":false,"member_count":30,"description":"Sales team"},
                {"name":"IT","type":"security","scope":"global","protected":false,"member_count":8,"description":"IT staff"},
                {"name":"Newsletter","type":"distribution","scope":"universal","protected":false,"member_count":120,"description":"Company news"},
            ])),
            "/api/ous" => ok(json!([
                {"dn":"CN=Users,DC=domain,DC=local","name":"Users","description":"Default container for users"},
                {"dn":"CN=Computers,DC=domain,DC=local","name":"Computers","description":"Default container for computers"},
                {"dn":"OU=Domain Controllers,DC=domain,DC=local","name":"Domain Controllers","description":"Default container for domain controllers"},
                {"dn":"OU=Example,DC=domain,DC=local","name":"Example","description":"Example Company Ltd"},
                {"dn":"OU=Finance,OU=Example,DC=domain,DC=local","name":"Finance","description":""},
                {"dn":"OU=Sales,OU=Example,DC=domain,DC=local","name":"Sales","description":""},
                {"dn":"OU=IT,OU=Example,DC=domain,DC=local","name":"IT","description":""},
            ])),
            "/api/computers" => ok(json!([
                {"name":"DC01","sam":"DC01$","dn":"CN=DC01,OU=Domain Controllers,DC=domain,DC=local","dns_name":"dc01.domain.local","os":"Windows Server 2022 Standard","os_version":"10.0 (20348)","enabled":true,"is_dc":true,"last_logon":"2026-10-22T07:00:00Z"},
                {"name":"FS01","sam":"FS01$","dn":"CN=FS01,CN=Computers,DC=domain,DC=local","dns_name":"fs01.domain.local","os":"Samba","os_version":"4.23","enabled":true,"is_dc":false,"last_logon":"2026-10-22T07:00:00Z"},
                {"name":"PC-FIN-01","sam":"PC-FIN-01$","dn":"CN=PC-FIN-01,CN=Computers,DC=domain,DC=local","dns_name":"pc-fin-01.domain.local","os":"Windows 11 Pro","os_version":"10.0 (26100)","enabled":true,"is_dc":false,"last_logon":"2026-10-20T08:00:00Z"},
                {"name":"PC-OLD-02","sam":"PC-OLD-02$","dn":"CN=PC-OLD-02,CN=Computers,DC=domain,DC=local","dns_name":"pc-old-02.domain.local","os":"Windows 7 Professional","os_version":"6.1 (7601)","enabled":false,"is_dc":false,"last_logon":"2025-01-10T08:00:00Z"},
            ])),
            "/api/agents" => ok(json!([{"id":1,"name":"FS01","url":"https://fs01.domain.local:8445"}])),
            "/api/policy" => ok(
                json!({"complexity":true,"min_length":7,"history":24,"min_age_days":1,"max_age_days":42,"lockout_threshold":10,"lockout_minutes":30,"lockout_window_minutes":30}),
            ),
            "/api/audit" => ok(json!([
                {"time":"2026-10-22T10:01:00Z","actor":"administrator","ip":"10.0.0.15","action":"user.password","target":"bruno.silva","result":"ok"},
                {"time":"2026-10-22T09:40:00Z","actor":"administrator","ip":"10.0.0.15","action":"group.member.add","target":"Finance","result":"ok"},
                {"time":"2026-10-22T09:12:00Z","actor":"ana.silva","ip":"10.0.0.21","action":"login","target":"panel","result":"user is not a domain administrator"},
            ])),
            "/api/samba/logs" | "/api/agents/1/samba/logs" => ok(
                json!({"dir":"/var/log/samba","files":[{"name":"log.samba","size":20480},{"name":"log.smbd","size":0}]}),
            ),
            "/api/backups" => ok(json!([
                {"id":0,"name":"panel","kinds":["panel"],"backups":[{"id":"panel-20261022-020000-a1b2c3d4","kind":"panel","origin":"manual","by":"administrator","file":"panel.tar.gz","size":5242880,"started":"2026-10-22T02:00:00Z","status":"ok"}]},
                {"id":1,"name":"FS01","kinds":["fileserver-config","fileserver-data"],"roots":["/srv/shares"],"upload_kinds":["fileserver-config"],"upload_max_mib":512,"backups":[
                    {"id":"fs01-data-20261021-010000-9f8e7d6c","kind":"fileserver-data","full":true,"origin":"schedule:nightly","file":"data.tar","size":10737418240_i64,"started":"2026-10-21T01:00:00Z","status":"ok","folders":["/srv/shares/finance"]}]},
            ])),
            "/api/agents/1/backup-schedules" => ok(
                json!([{"id":"nightly","kind":"fileserver-data","cron":"0 1 * * *","retention":{"chains":4,"days":0},"enabled":true}]),
            ),
            "/api/agents/1/syncs" => ok(
                json!({"jobs":[{"id":"finance","name":"finance · old-fs","rev":2,"approved_rev":2,"source":{"host":"old-fs.domain.local","share":"dados","path":"finance","auth":"kerberos"},
                "dest":"/srv/shares/finance","mode":"mirror","cron":"0 1 * * *","enabled":true,"max_delete":100,"max_delete_pct":20,"reinherit":true}],
                "runs":[{"id":"r1","job":"finance","preview":false,"started":"2026-10-22T01:00:00Z","finished":"2026-10-22T01:04:38Z","status":"ok",
                "result":{"plan":{"New":12,"Changed":3,"Delete":1,"Same":5400},"transfers":15,"deleted":1}}],"roots":["/srv/shares"],"file_server":true,"kerberos":true,"machine":"FS01$"}),
            ),
            "/api/agents/1/shares" => ok(json!([
                {"name":"finance","path":"/srv/shares/finance","comment":"Finance","rw_groups":["DOMAIN\\Finance"],"ro_groups":["DOMAIN\\Auditors"],"browseable":true},
                {"name":"public","path":"/srv/shares/public","comment":"Everyone","rw_groups":["DOMAIN\\Domain Users"],"ro_groups":[],"browseable":true},
                {"name":"it$","path":"/srv/shares/it","comment":"IT tools","rw_groups":["DOMAIN\\IT"],"ro_groups":[],"browseable":false},
            ])),
            "/api/agents/1/sizes" => ok(json!({"sizes":{"finance":12884901888_i64,"public":734003200,"it$":52428800}})),
            p if p.starts_with("/api/agents/1/tasks/") => ok(json!({"id":"t1","state":"completed"})),
            p if p.starts_with("/api/users/") => {
                let sam = p.trim_start_matches("/api/users/");
                match list.iter().find(|u| u["sam"].as_str().is_some_and(|s| s.eq_ignore_ascii_case(sam))) {
                    Some(u) => ok(u.clone()),
                    None => (404, json!({"error": "user not found"}).to_string()),
                }
            }
            p if p.starts_with("/api/groups/") => ok(
                json!({"name": p.trim_start_matches("/api/groups/"), "members": ["ana.silva", "bruno.silva"], "description": ""}),
            ),
            _ => (404, json!({"error": format!("no route {p}")}).to_string()),
        }
    })
}
