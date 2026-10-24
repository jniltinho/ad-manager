//! Files the operator picks: a download saved where they choose (CSV export, backups, quarantine files) and a file
//! to send (a backup). The native dialog and the transfer run off the UI thread; the transfer has no total timeout.

use std::path::PathBuf;

use adm_core::api::Api;

use crate::app::{App, Cx};

/// Asks where to save `name`, then streams GET `path` there; `done` gets the file written.
pub fn save_download(
    cx: &Cx,
    api: Api,
    path: String,
    name: String,
    done: impl FnOnce(&mut App, PathBuf) + Send + 'static,
) {
    cx.run(
        async move {
            let Some(h) = rfd::AsyncFileDialog::new().set_file_name(&name).save_file().await else { return Ok(None) };
            let p = h.path().to_path_buf();
            api.download(&path, &p).await?;
            Ok(Some(p))
        },
        move |app, r| {
            if let Some(p) = r {
                done(app, p);
            }
        },
    );
}

/// Asks for a file to open; `done` gets it (nothing when the operator cancels, or when the session that asked was
/// replaced meanwhile).
pub fn pick_file(cx: &Cx, done: impl FnOnce(&mut App, PathBuf) + Send + 'static) {
    let session = cx.panel.id;
    cx.spawn(
        async move { rfd::AsyncFileDialog::new().pick_file().await.map(|h| h.path().to_path_buf()) },
        move |app, r| {
            if let Some(p) = r
                && app.cx.panel.id == session
            {
                done(app, p);
            }
        },
    );
}

/// GET /api/export/<file> (the CSV the CLI import reads) saved where the operator picks.
pub fn export(cx: &Cx, file: &str, name: &str) {
    let path = format!("/api/export/{file}");
    save_download(cx, cx.panel.clone(), path, name.into(), |app, p| {
        app.cx.set_status(adm_core::tf!("Exported to %s", p.display()));
    });
}

/// Asks where to save `name` and writes `rows` (the header first) there as the web's CSV export: `;` between
/// fields, every field quoted, a BOM for Excel and CRLF line ends.
pub fn save_csv(cx: &Cx, name: &str, rows: Vec<Vec<String>>) {
    let (name, text) = (name.to_string(), csv_text(&rows));
    cx.run(
        async move {
            let Some(h) = rfd::AsyncFileDialog::new().set_file_name(&name).save_file().await else { return Ok(None) };
            let p = h.path().to_path_buf();
            tokio::fs::write(&p, text).await.map_err(|e| adm_core::api::ApiError::Other(e.to_string()))?;
            Ok(Some(p))
        },
        |app, r| {
            if let Some(p) = r {
                app.cx.set_status(adm_core::tf!("Exported to %s", p.display()));
            }
        },
    );
}

fn csv_text(rows: &[Vec<String>]) -> String {
    let line =
        |r: &Vec<String>| r.iter().map(|v| format!("\"{}\"", v.replace('"', "\"\""))).collect::<Vec<_>>().join(";");
    format!("\u{feff}{}", rows.iter().map(line).collect::<Vec<_>>().join("\r\n"))
}

#[cfg(test)]
mod tests {
    #[test]
    fn csv_as_the_web() {
        let rows = vec![vec!["Date".into(), "Target".into()], vec!["1".into(), "a \"b\"; c".into()]];
        assert_eq!(super::csv_text(&rows), "\u{feff}\"Date\";\"Target\"\r\n\"1\";\"a \"\"b\"\"; c\"");
    }
}
