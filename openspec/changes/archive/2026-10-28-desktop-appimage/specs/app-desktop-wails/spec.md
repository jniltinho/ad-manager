## MODIFIED Requirements

### Requirement: Wails desktop configuration file
The app SHALL read and write the same `desktop.toml` as the Rust app, with the same keys, legacy keys and lookup
order (`--config`, `AD_MANAGER_DESKTOP`, `.config/desktop.toml` beside the executable, the user config folder); a Linux AppImage and
a macOS `.app` SHALL use the user config folder instead of a file beside the executable. It
SHALL write the file atomically with mode `0600`, keep keys and sections it does not use, and refuse a file
readable by group or others (Unix).

#### Scenario: Shared with the Rust app
- **WHEN** the Wails app starts with a `desktop.toml` saved by the Rust app that has `[graphics] gpu_backend`
- **THEN** it uses the same panel URL, user, fingerprint, language and appearance, and a Save keeps the
  `[graphics] gpu_backend` key

#### Scenario: Loose permissions
- **WHEN** the file has mode `0644`
- **THEN** the Configure screen shows "<path> must be private and has mode 0644: run chmod 600" and no panel
  request is made

#### Scenario: First run
- **WHEN** no file exists
- **THEN** the Configure screen opens unauthenticated with user `administrator`, no panel request starts, and
  nothing but language, appearance or graphics preferences is written before a successful test and Save

#### Scenario: Preferences only
- **WHEN** the file has `lang` and `[appearance]` but no `[panel] url`
- **THEN** the app starts on Configure, as unconfigured, in that language and theme

#### Scenario: AppImage
- **WHEN** the Linux AppImage starts with `~/.config/ad-manager/desktop.toml` filled in, or with no file at all
- **THEN** it reads, or at the first Save writes, `~/.config/ad-manager/desktop.toml`, never a file beside the
  `.AppImage` or inside its mounted files
