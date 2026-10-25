# Desktop app

For the manager to use on their own machine (Linux, Windows or macOS), without a browser. It is written in Rust with
[egui](https://github.com/emilk/egui) ([`desktop/`](../desktop/README.md)) and is a client of the **panel
API** only (sign-in with the domain account, same rules — *Domain Admins* only, audit with the machine's IP): the file
servers' shares and Samba logs go through the panel's proxy to each agent too, so the desktop needs only the panel's
port and never holds an agent token.

| Sign-in | Users |
|---|---|
| ![sign-in](screenshots/desktop-rs-01-login.png) | ![users](screenshots/desktop-rs-04-users.png) |

More screens in [screenshots/](screenshots/README.md#desktop-app).

## What it does

The web panel's pages and dialogs, laid out as the web's, against the same API and with the same server protections:
**Home**, **Users**, **Groups**, **OUs** (with GPOs), **Computers** (with DNS and SMB sessions), **Shares** (file
servers, tasks, sessions, the share form with the folder's permissions), **Import/export** (CSV, another AD, folders,
each previewed as a plan), **Backup** and **Sync** (the wizard with its preview step, runs, quarantine), **Logs**
(audit, HTTP, Samba) and **Settings** (language, theme, company, logo, agent token), with the web's guards (built-in
and own accounts, DCs, typed confirmations).

**Same themes as the panel**: the 20 themes of the web panel, with their gradients, shadows, fonts and page pattern,
or flat. The app follows the theme chosen in the panel's **Settings**; **Configure › Appearance** picks another theme
(and flat or with depth) for this computer only, applied at once and saved as `[appearance]` in desktop.toml (*Panel
default* follows the panel again). Saving a new theme in **Settings** from the app also hands this computer back to
the panel's theme. The colors come from the web's CSS (`desktop/crates/core/assets/themes.json`, regenerated
with `make desktop-themes`; `make desktop-check` fails when it is out of date).

**Crash-safe graphics start**: the app renders through wgpu and, when a start dies in the graphics driver, the next one
tries a safer backend (down to software rendering); **Configure › Graphics** chooses it by hand. Details in
[desktop/README.md](../desktop/README.md).

Download, install, upgrade and removal on each system: [install-guide.md](install-guide.md#3-desktop-app).

## Configure

On first run the app opens the **Configure** screen: panel URL, user, password and language. **Test connection**
reads the panel's certificate, checks its health and signs in with that account (*Domain Admins*), showing the domain,
the user and the panel version, or the panel's reason (wrong password, not a Domain Admin, panel being set up).
**Save** is enabled after a successful test of the values on screen: it writes the configuration and opens the app
already signed in, without asking for the password again. The password is used only for the test and the session and
is never saved.

**Self-signed certificate.** When this computer does not trust the panel's certificate (a fresh installation's own),
Test shows its SHA-256 fingerprint, subject, issuer and validity. Compare the fingerprint with the panel's (the panel
logs it at start: `journalctl -u ad-manager | grep sha256`) and click **Trust this certificate**: only that certificate
is accepted from then on (`fingerprint` in the file). A renewed or replaced certificate is refused until it is trusted
again in **Configure**. The app never follows a redirect from the panel.

On save, it writes **`.config/desktop.toml` in the executable's folder** (permission `0600`). This way each copy of
the app carries its own configuration, even on a USB stick. To change it later: **Configure** on the sign-in screen
or in the header. If the file has looser permissions, the app refuses to open.

Lookup order:

1. `ad-manager-desktop --config /path/desktop.toml`
2. variable `AD_MANAGER_DESKTOP=/path/desktop.toml`
3. `.config/desktop.toml` next to the executable
4. the user's configuration folder: `~/.config/ad-manager/desktop.toml` (Linux),
   `~/Library/Application Support/ad-manager/desktop.toml` (macOS), `%AppData%\ad-manager\desktop.toml` (Windows)

If the executable's folder is not writable (`Program Files`, `/usr/bin`) or in the macOS `.app`, the new file goes to
the location of item 4. On Windows the permission follows the folder's: keep the app in a folder of your own.

```toml
lang = "en"               # "pt-BR" or "en"; left out = the panel's default

[panel]
url = "https://panel.example.local:8443"
fingerprint = "sha256:…"  # only for a self-signed certificate, written by Configure
user = "administrator"    # the password is asked on sign-in and stays only in memory

[appearance]              # written by Configure › Appearance; left out = the panel's theme
theme = "breeze-dark"
```

**File servers.** They are registered once in the web panel (**Shares → File servers → + new**, with the agent's
token); the desktop lists them from the panel.

## Build

Every recipe runs in Docker (the pinned Rust toolchain image): nothing to install but Docker.

```bash
make desktop          # Linux tar.gz, Windows zip and the Linux binary in dist/desktop/
make desktop-check    # format, clippy, unit and UI acceptance tests, cargo-deny, themes in step with the CSS
make desktop-shots    # the docs screenshots (2×, Classic blue, domain.local)
make desktop-themes   # themes.json again from the web's theme CSS
```

macOS builds natively on a Mac (`cd desktop && cargo build --release`, then `packaging/macos/bundle.sh` for the
universal `.app`). The desktop app has the server's version and comes in the same release: a tag `vX.Y.Z`
(`release.yml`) publishes `ad-manager-desktop_X.Y.Z_linux_x86_64.tar.gz`, `…_windows_x86_64.zip` and
`…_macos_universal.zip` beside the server's packages, with one `checksums.txt`. More (crates, live test against the test image, benchmark): [desktop/README.md](../desktop/README.md).

## The Fyne app (archived)

The first desktop app, written in Go with [Fyne](https://fyne.io), is kept in [`archive/desktop-go/`](../archive/desktop-go) only as code to
study: it is no longer built, tested or released, and its screenshots were replaced by the Rust app's. Its theme
generator (`archive/desktop-go/internal/themegen`) still produces the Rust app's `themes.json`.
