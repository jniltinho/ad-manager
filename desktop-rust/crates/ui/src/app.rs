//! The application: its screens (Configure, sign-in, the shell), the services every screen uses ([`Cx`]: the panel
//! session, the background runtime, the status line, dialogs, the theme) and the frame loop.
//!
//! Threading (command pattern): requests run on a tokio runtime; each one ends by sending a [`Job`] — a closure over
//! `&mut App` — on a channel. The UI drains the channel at the start of every frame, so only the UI thread ever
//! changes UI state and no lock guards it. A job of a session that was replaced meanwhile is dropped.

use std::any::Any;
use std::future::Future;
use std::path::PathBuf;
use std::sync::Arc;
use std::sync::mpsc::{Receiver, Sender, channel};

use adm_core::api::{Api, ApiError, ApiResult};
use adm_core::config::Config;
use adm_core::i18n::{self, t};
use adm_core::model::Me;
use adm_platform::Platform;
use egui::{Align2, Context, Ui};

use crate::modal::{self, Dialog, Flow};
use crate::style::{self, Look};
use crate::{login, setup, shell};

/// Work handed back to the UI thread.
pub type Job = Box<dyn FnOnce(&mut App) + Send>;

/// What a screen asks the app to switch to (applied after the frame).
pub enum Go {
    Login,
    /// The shell with this session (a successful sign-in or Configure test).
    Shell(Box<Api>, Box<Me>),
    /// Back to the shell kept aside while Configure was open (only the language changed).
    KeepShell,
    /// Configure (from the shell, the shell is kept to come back to).
    Setup,
}

pub enum Screen {
    /// The configuration could not be read: only the reason is shown.
    Failed(String),
    Setup(Box<setup::Setup>),
    Login(Box<login::Login>),
    Shell(Box<shell::Shell>),
}

/// The services of every screen.
pub struct Cx {
    pub platform: Arc<dyn Platform>,
    pub ctx: Context,
    rt: tokio::runtime::Handle,
    tx: Sender<Job>,
    pub cfg: Config,
    pub cfg_path: PathBuf,
    pub panel: Api,
    pub me: Me,
    pub look: Look,
    /// The status line, and the hint of the link under the pointer (shown instead while it is there).
    pub status: String,
    pub hover: Option<String>,
    /// Open dialogs, top last, each with its instance id (a reply goes to the instance that asked, never to another
    /// of the same type opened later).
    pub dialogs: Vec<(u64, Box<dyn Dialog>)>,
    next_dialog: u64,
    /// The dialog being drawn (its id), for the requests it starts.
    drawing: Option<u64>,
    pub go: Option<Go>,
    /// Said once in the status line after sign-in (the config's agent tokens were removed).
    pub notice: Option<(String, String)>,
    /// The ad-manager-desktop-rs version, for the footer.
    pub version: &'static str,
    /// The panel's public brand for the sign-in screen.
    pub brand: crate::login::PublicBrand,
    /// The window title wanted (sent to the window when it changes).
    pub title: String,
    /// Notices of the graphics start: the fallback is shown under the sign-in box, and all go to the status line
    /// when the shell opens.
    pub gpu_notes: Vec<GpuNote>,
}

/// A notice of the graphics start, translated when shown (the language can change after the start).
pub enum GpuNote {
    /// The start on the first backend died; now the second.
    Fallback(adm_core::gpu::Backend, adm_core::gpu::Backend),
    /// The marker could not be taken or written: no crash protection for this start.
    Unprotected(String),
    /// The working fallback could not be saved to desktop.toml.
    SaveFailed(String),
}

impl GpuNote {
    pub fn text(&self) -> String {
        match self {
            Self::Fallback(crashed, now) => adm_core::tf!(
                "The graphics driver stopped the last start (%s); now using %s. Change it in Configure.",
                crashed.name(),
                now.name()
            ),
            Self::Unprotected(e) => format!("{}{e}", t("Graphics start protection is off for this start: ")),
            Self::SaveFailed(e) => format!("{}{e}", t("The graphics choice could not be saved: ")),
        }
    }
}

impl Cx {
    /// Runs `fut` off the UI thread and hands its output to `done` on the UI thread.
    pub fn spawn<T: Send + 'static>(
        &self,
        fut: impl Future<Output = T> + Send + 'static,
        done: impl FnOnce(&mut App, T) + Send + 'static,
    ) {
        let tx = self.tx.clone();
        let ctx = self.ctx.clone();
        self.rt.spawn(async move {
            let v = fut.await;
            let _ = tx.send(Box::new(move |app: &mut App| done(app, v)));
            ctx.request_repaint();
        });
    }

    /// Runs a panel request of the current session: an error is shown (an expired session goes back to the sign-in
    /// screen), a success goes to `ok`; nothing happens when the session was replaced meanwhile.
    pub fn run<T: Send + 'static>(
        &self,
        fut: impl Future<Output = ApiResult<T>> + Send + 'static,
        ok: impl FnOnce(&mut App, T) + Send + 'static,
    ) {
        let session = self.panel.id;
        self.spawn(fut, move |app, r| {
            if app.cx.panel.id != session {
                return;
            }
            match r {
                Ok(v) => ok(app, v),
                Err(e) => app.cx.handle_error(e),
            }
        });
    }

    /// Runs `f` on the app after this frame (an action that must look at a page's current state).
    pub fn defer(&self, f: impl FnOnce(&mut App) + Send + 'static) {
        let _ = self.tx.send(Box::new(f));
        self.ctx.request_repaint();
    }

    /// The tokio runtime (long polls, file dialogs).
    pub fn rt(&self) -> &tokio::runtime::Handle {
        &self.rt
    }

    pub fn set_status(&mut self, s: impl Into<String>) {
        self.status = s.into();
    }

    /// Opens a dialog; returns its instance id.
    pub fn push(&mut self, d: impl Dialog + 'static) -> u64 {
        self.next_dialog += 1;
        self.dialogs.push((self.next_dialog, Box::new(d)));
        self.next_dialog
    }

    /// The id of the dialog being drawn (0 outside a dialog).
    pub fn current_dialog(&self) -> u64 {
        self.drawing.unwrap_or(0)
    }

    /// A fresh session for the configured panel: a new cookie jar and identity, so nothing still running for the
    /// session before it can reach the screens of the next one.
    pub fn renew_session(&mut self) {
        let c = &self.cfg.panel;
        if let Ok(a) = Api::for_panel(&c.url, &c.fingerprint, c.insecure) {
            self.panel = a;
        }
    }

    /// Fetches the panel's catalog of server messages once per panel (also when a session starts without the
    /// sign-in screen: Configure's Save).
    pub fn ensure_catalog(&mut self) {
        if self.brand.catalog {
            return;
        }
        let api = self.panel.clone();
        let url = self.cfg.panel.url.clone();
        self.spawn(async move { api.server_catalog().await }, move |app, r| {
            if let Ok(c) = r
                && app.cx.cfg.panel.url == url
            {
                i18n::set_server_catalog(c);
                app.cx.brand.catalog = true;
            }
        });
    }

    pub fn show_info(&mut self, title: impl Into<String>, msg: impl Into<String>) {
        self.push(modal::Info::new(title, msg));
    }

    pub fn show_error(&mut self, e: ApiError) {
        self.push(modal::ErrorBox(e));
    }

    pub fn show_error_text(&mut self, s: impl Into<String>) {
        self.push(modal::ErrorBox(ApiError::Other(s.into())));
    }

    /// An error: an expired session goes back to the sign-in screen, anything else is shown.
    pub fn handle_error(&mut self, e: ApiError) {
        if e == ApiError::SessionExpired {
            self.go = Some(Go::Login);
            return;
        }
        self.show_error(e);
    }

    /// Asks before running `action`; `danger` paints the dialog red.
    pub fn confirm(
        &mut self,
        title: &str,
        msg: String,
        button: &str,
        danger: bool,
        action: impl FnOnce(&mut Cx) + 'static,
    ) {
        self.push(modal::Confirm::new(title, msg, button, danger, action));
    }

    /// Guards a destructive action: it only runs once `expected` is typed (case-insensitive).
    pub fn confirm_typed(&mut self, title: &str, msg: String, expected: &str, action: impl FnOnce(&mut Cx) + 'static) {
        self.push(modal::ConfirmTyped::new(title, msg, expected, action));
    }

    pub fn set_look(&mut self, id: &str, flat: bool) {
        if let Some(l) = Look::with_flat(id, flat)
            && (l.id != self.look.id || l.flat != self.look.flat)
        {
            l.apply(&self.ctx);
            self.look = l;
        }
    }

    /// The dialog `id` if it is still open and of type `T` (to deliver a result to it).
    pub fn dialog_mut<T: Dialog + 'static>(&mut self, id: u64) -> Option<&mut T> {
        self.dialogs.iter_mut().find(|d| d.0 == id).and_then(|d| (d.1.as_mut() as &mut dyn Any).downcast_mut::<T>())
    }

    pub fn inactive_days(&self) -> i64 {
        adm_core::logic::inactive_days(&self.me)
    }
}

/// Why the app could not start.
#[derive(Debug, thiserror::Error)]
pub enum StartError {
    #[error("cannot start the background runtime: {0}")]
    Runtime(#[from] std::io::Error),
    #[error("the bundled themes are missing")]
    Theme,
    #[error("cannot build the HTTP client: {0}")]
    Client(#[from] ApiError),
}

pub struct App {
    pub cx: Cx,
    pub screen: Screen,
    rx: Receiver<Job>,
    shown_title: String,
    /// When the last frame started (frame pacing).
    last_frame: Option<std::time::Instant>,
    /// The shell kept aside while Configure is open from the header.
    kept: Option<Box<shell::Shell>>,
    /// Modal heights of the last frame (to keep their top at 10% of the window when they fit).
    heights: Vec<f32>,
    /// The graphics start until it counts as working.
    gpu: Option<GpuStart>,
    /// The client width last seen (points before zoom) and since when, for the web's zoom steps.
    zoom_seen: (f32, std::time::Instant),
    _rt: Option<tokio::runtime::Runtime>,
}

/// How the app starts.
pub struct Start {
    pub platform: Arc<dyn Platform>,
    pub config: Result<adm_core::config::Loaded, String>,
    pub version: &'static str,
    /// The crash-safe graphics start (none in tests).
    pub gpu: Option<GpuStart>,
}

/// The crash-safe graphics start handed over by the binary (design D12; the plan and the marker are `adm_core::gpu`).
pub struct GpuStart {
    pub plan: adm_core::gpu::Plan,
    /// Why this start has no crash protection (the marker could not be taken or written), for the status line.
    pub warning: Option<String>,
    /// Clears the marker; called once [`adm_core::gpu::START_FRAMES`] frames painted.
    pub on_started: Box<dyn FnOnce() + Send>,
}

impl App {
    pub fn new(ctx: &Context, start: Start) -> Result<Self, StartError> {
        let rt = tokio::runtime::Builder::new_multi_thread().worker_threads(2).enable_all().build()?;
        Self::with_runtime(ctx, start, rt.handle().clone(), Some(rt))
    }

    /// The app on an existing runtime (tests).
    pub fn with_runtime(
        ctx: &Context,
        start: Start,
        rt: tokio::runtime::Handle,
        own: Option<tokio::runtime::Runtime>,
    ) -> Result<Self, StartError> {
        egui_extras::install_image_loaders(ctx);
        let (tx, rx) = channel();
        let look = Look::new("default").ok_or(StartError::Theme)?;
        look.apply(ctx);
        let (cfg, cfg_path, needs_setup, notice, failed) = match start.config {
            Ok(l) => (l.config, l.path, l.needs_setup, l.notice, None),
            Err(e) => (Config::default(), PathBuf::new(), false, None, Some(e)),
        };
        i18n::set_lang(&cfg.lang);
        let panel = Api::for_panel(&cfg.panel.url, &cfg.panel.fingerprint, cfg.panel.insecure)?;
        let cx = Cx {
            platform: start.platform,
            ctx: ctx.clone(),
            rt,
            tx,
            cfg,
            cfg_path,
            panel,
            me: Me::default(),
            look,
            status: String::new(),
            hover: None,
            dialogs: Vec::new(),
            next_dialog: 0,
            drawing: None,
            go: None,
            notice,
            version: start.version,
            brand: Default::default(),
            title: String::new(),
            gpu_notes: Vec::new(),
        };
        let mut app = Self {
            cx,
            screen: Screen::Failed(String::new()),
            rx,
            shown_title: String::new(),
            last_frame: None,
            kept: None,
            heights: Vec::new(),
            gpu: None,
            zoom_seen: (0.0, std::time::Instant::now()),
            _rt: own,
        };
        if let Some(g) = start.gpu {
            if let Some((crashed, now)) = g.plan.notice() {
                app.cx.gpu_notes.push(GpuNote::Fallback(crashed, now));
            }
            if let Some(w) = &g.warning {
                app.cx.gpu_notes.push(GpuNote::Unprotected(w.clone()));
            }
            app.gpu = Some(g);
        }
        app.screen = match failed {
            Some(e) => Screen::Failed(e),
            None if needs_setup => Screen::Setup(Box::new(setup::Setup::new(&app.cx, setup::Back::None))),
            None => Screen::Login(Box::new(login::Login::new(&mut app.cx))),
        };
        Ok(app)
    }

    /// The web's zoom steps (design D6): from the client width before zoom — the content rect in points times the
    /// current zoom, never the monitor — 1.6 from 3200, 1.25 from 2400, else 1; applied once the size has stayed for
    /// 300 ms, so a resize does not relayout at every step of the drag.
    fn zoom(&mut self, ctx: &Context) {
        let w = ctx.content_rect().width() * ctx.zoom_factor();
        let now = std::time::Instant::now();
        if (w - self.zoom_seen.0).abs() > 0.5 {
            self.zoom_seen = (w, now);
            ctx.request_repaint_after(std::time::Duration::from_millis(320));
            return;
        }
        let z = zoom_step(w);
        if now.duration_since(self.zoom_seen.1) >= std::time::Duration::from_millis(300)
            && (ctx.zoom_factor() - z).abs() > 1e-3
        {
            ctx.set_zoom_factor(z);
        }
    }

    /// Once [`adm_core::gpu::START_FRAMES`] frames completed (and eframe painted them: a frame's extra layout passes
    /// do not count) the graphics start counts as working: the marker is emptied, and a hardware fallback reached
    /// after a crash is saved as `[graphics]` in desktop.toml — when the file exists; on a first run it is kept for
    /// Configure's Save (nothing is written before it). A write error becomes a notice.
    pub fn gpu_started(&mut self, ctx: &Context) {
        if self.gpu.is_none() {
            return;
        }
        if ctx.cumulative_frame_nr() < u64::from(adm_core::gpu::START_FRAMES) {
            ctx.request_repaint();
            return;
        }
        let Some(g) = self.gpu.take() else { return };
        if let Some(b) = g.plan.to_save() {
            adm_core::gpu::set_choice(&mut self.cx.cfg, b);
            if self.cx.cfg_path.exists()
                && let Err(e) = adm_core::config::save(self.cx.platform.as_ref(), &self.cx.cfg_path, &self.cx.cfg)
            {
                self.cx.set_status(GpuNote::SaveFailed(e.to_string()).text());
                self.cx.gpu_notes.push(GpuNote::SaveFailed(e.to_string()));
            }
        }
        (g.on_started)();
    }

    pub fn shell(&mut self) -> Option<&mut shell::Shell> {
        match &mut self.screen {
            Screen::Shell(s) => Some(s),
            _ => None,
        }
    }

    /// Runs `f` on dialog `id` (when still open and a `T`) with the services: it is taken out of the stack meanwhile.
    pub fn with_dialog<T: Dialog + 'static>(&mut self, id: u64, f: impl FnOnce(&mut T, &mut Cx)) {
        let Some(i) = self.cx.dialogs.iter().position(|d| d.0 == id) else { return };
        let (id, mut d) = self.cx.dialogs.remove(i);
        if let Some(t) = (d.as_mut() as &mut dyn Any).downcast_mut::<T>() {
            let before = self.cx.drawing.replace(id);
            f(t, &mut self.cx);
            self.cx.drawing = before;
        }
        let at = i.min(self.cx.dialogs.len());
        self.cx.dialogs.insert(at, (id, d));
    }

    /// Runs `f` on the shell and the services together (a page reacting to a finished request).
    pub fn with_shell(&mut self, f: impl FnOnce(&mut shell::Shell, &mut Cx)) {
        if let Screen::Shell(s) = &mut self.screen {
            f(s, &mut self.cx);
        }
    }

    /// Applies the jobs finished since the last frame.
    pub fn pump(&mut self) {
        while let Ok(job) = self.rx.try_recv() {
            job(self);
        }
    }

    /// One frame.
    pub fn frame(&mut self, ui: &mut Ui) {
        self.zoom(ui.ctx());
        self.pump();
        self.cx.hover = None;
        let modal_open = !self.cx.dialogs.is_empty();
        match &mut self.screen {
            Screen::Failed(msg) => failed(ui, &self.cx.look, msg),
            Screen::Setup(s) => s.ui(ui, &mut self.cx),
            Screen::Login(l) => l.ui(ui, &mut self.cx, modal_open),
            Screen::Shell(s) => s.ui(ui, &mut self.cx, modal_open),
        }
        self.dialogs(ui.ctx());
        style::focus_ring(ui.ctx(), &self.cx.look);
        self.switch();
        if self.cx.title != self.shown_title {
            self.shown_title = self.cx.title.clone();
            ui.ctx().send_viewport_cmd(egui::ViewportCommand::Title(self.shown_title.clone()));
        }
    }

    /// Draws the open dialogs, top one last; each is taken out while it draws so it can use the services.
    fn dialogs(&mut self, ctx: &Context) {
        let mut i = 0;
        let screen = ctx.content_rect();
        while i < self.cx.dialogs.len() {
            let (did, mut d) = self.cx.dialogs.remove(i);
            self.cx.drawing = Some(did);
            let top = i == self.cx.dialogs.len();
            let last_h = self.heights.get(i).copied().unwrap_or(0.0);
            let y = (screen.height() * 0.1).min(screen.height() - last_h).max(0.0);
            let id = egui::Id::new(("adm-dialog", i));
            let area = egui::Modal::default_area(id).anchor(Align2::CENTER_TOP, egui::vec2(0.0, y));
            let mut flow = Flow::Keep;
            let r = egui::Modal::new(id)
                .area(area)
                .frame(egui::Frame::NONE)
                .backdrop_color(self.cx.look.c("backdrop"))
                .show(ctx, |ui| flow = modal::frame(ui, d.as_mut(), &mut self.cx));
            self.cx.drawing = None;
            // Escape closes the top dialog; a click outside does not (as the Fyne app)
            if top && ctx.input(|inp| inp.key_pressed(egui::Key::Escape)) {
                flow = Flow::Close;
            }
            if self.heights.len() <= i {
                self.heights.resize(i + 1, 0.0);
            }
            self.heights[i] = r.response.rect.height();
            match flow {
                Flow::Keep => {
                    self.cx.dialogs.insert(i, (did, d));
                    i += 1;
                }
                Flow::Close => d.closed(ctx),
            }
        }
        self.heights.truncate(self.cx.dialogs.len());
    }

    /// Applies a screen switch a screen asked for during the frame.
    fn switch(&mut self) {
        let Some(go) = self.cx.go.take() else { return };
        // a screen that goes forgets its password fields (egui keeps their text and undo history)
        match &mut self.screen {
            Screen::Login(l) => l.forget(&self.cx.ctx),
            Screen::Setup(s) => s.forget(&self.cx.ctx),
            _ => {}
        }
        match go {
            Go::Login => {
                for (_, d) in self.cx.dialogs.drain(..) {
                    d.closed(&self.cx.ctx);
                }
                self.kept = None;
                // a new session: whatever still runs for the old one is dropped when it ends
                self.cx.renew_session();
                self.screen = Screen::Login(Box::new(login::Login::new(&mut self.cx)));
            }
            Go::Shell(api, me) => {
                if self.cx.panel.id != api.id {
                    let old = std::mem::replace(&mut self.cx.panel, *api);
                    self.cx.spawn(async move { old.logout().await }, |_, ()| {});
                }
                self.cx.me = *me;
                self.kept = None;
                self.enter_shell();
            }
            Go::KeepShell => match self.kept.take() {
                Some(s) => self.screen = Screen::Shell(s),
                None => self.enter_shell(),
            },
            Go::Setup => {
                let old = std::mem::replace(&mut self.screen, Screen::Failed(String::new()));
                let back = match old {
                    Screen::Shell(s) => {
                        self.kept = Some(s);
                        setup::Back::Shell
                    }
                    _ => setup::Back::Login,
                };
                self.screen = Screen::Setup(Box::new(setup::Setup::new(&self.cx, back)));
            }
        }
    }

    fn enter_shell(&mut self) {
        if self.cx.cfg.lang.is_empty() {
            i18n::set_lang(&self.cx.me.settings.lang);
        }
        let theme = self.cx.me.brand.theme.clone();
        self.cx.set_look(&theme, self.cx.me.brand.flat);
        for (_, d) in self.cx.dialogs.drain(..) {
            d.closed(&self.cx.ctx);
        }
        self.cx.ensure_catalog();
        self.cx.set_status(format!("{}{}", t("Connected to "), self.cx.cfg.panel.url));
        if let Some((msg, err)) = self.cx.notice.take() {
            self.cx.set_status(format!("{}{err}", t(&msg)));
        }
        if !self.cx.gpu_notes.is_empty() {
            let notes: Vec<String> = self.cx.gpu_notes.drain(..).map(|n| n.text()).collect();
            self.cx.set_status(notes.join("  ·  "));
        }
        self.screen = Screen::Shell(Box::new(shell::Shell::new(&mut self.cx)));
    }
}

/// The shortest time between two frames: 60 per second. A display with vsync already paces frames so; without it
/// (VMs, remote desktops, Xvfb) egui would draw as fast as it can while an animation or the wheel smoothing runs.
const MIN_FRAME: std::time::Duration = std::time::Duration::from_micros(16_600);

impl eframe::App for App {
    fn ui(&mut self, ui: &mut Ui, _frame: &mut eframe::Frame) {
        if let Some(last) = self.last_frame {
            let since = last.elapsed();
            if since < MIN_FRAME {
                std::thread::sleep(MIN_FRAME - since);
            }
        }
        self.last_frame = Some(std::time::Instant::now());
        self.frame(ui);
        self.gpu_started(&ui.ctx().clone());
    }

    fn raw_input_hook(&mut self, ctx: &Context, raw: &mut egui::RawInput) {
        let line = ctx.options(|o| o.input_options.line_scroll_speed);
        immediate_wheel(&mut raw.events, line);
    }
}

/// The web's zoom for a client width in points before zoom (style.css: 1.25 from 2400 px, 1.6 from 3200 px).
pub fn zoom_step(w: f32) -> f32 {
    if w >= 3200.0 {
        1.6
    } else if w >= 2400.0 {
        1.25
    } else {
        1.0
    }
}

/// A mouse wheel notch scrolls at once, in one frame, as in the Fyne app. egui spreads a notch (a "line", what X11
/// and Windows wheels report) over several frames, each a redraw of the whole window; it applies at once the small
/// steps a touchpad sends, so a notch is handed over as those (under 8 points each).
pub fn immediate_wheel(events: &mut Vec<egui::Event>, line_speed: f32) {
    if !events.iter().any(|e| matches!(e, egui::Event::MouseWheel { unit: egui::MouseWheelUnit::Line, .. })) {
        return;
    }
    let mut out = Vec::with_capacity(events.len() + 16);
    for e in events.drain(..) {
        match e {
            egui::Event::MouseWheel { unit: egui::MouseWheelUnit::Line, delta, phase, modifiers } => {
                let px = delta * line_speed;
                let n = (px.length() / 7.0).ceil().max(1.0);
                for _ in 0..n as usize {
                    out.push(egui::Event::MouseWheel {
                        unit: egui::MouseWheelUnit::Point,
                        delta: px / n,
                        phase,
                        modifiers,
                    });
                }
            }
            other => out.push(other),
        }
    }
    *events = out;
}

fn failed(ui: &mut Ui, look: &Look, msg: &str) {
    egui::CentralPanel::default().show(ui, |ui| {
        ui.vertical_centered(|ui| {
            ui.add_space(ui.available_height() * 0.3);
            crate::widgets::card(ui, look, t("Configuration"), |ui| {
                ui.set_max_width(640.0);
                ui.add(egui::Label::new(msg).wrap());
            });
        });
    });
}

#[cfg(test)]
mod tests {
    use super::immediate_wheel;
    use egui::{Event, Modifiers, MouseWheelUnit, TouchPhase, vec2};

    #[test]
    fn zoom_steps_at_the_web_boundaries() {
        use super::zoom_step;
        assert_eq!((zoom_step(1920.0), zoom_step(2399.0), zoom_step(2400.0)), (1.0, 1.0, 1.25));
        assert_eq!((zoom_step(3199.0), zoom_step(3200.0), zoom_step(3840.0)), (1.25, 1.6, 1.6));
    }

    #[test]
    fn a_notch_becomes_small_steps_of_the_same_total() {
        let mut ev = vec![
            Event::MouseWheel {
                unit: MouseWheelUnit::Line,
                delta: vec2(0.0, -1.0),
                phase: TouchPhase::Move,
                modifiers: Modifiers::NONE,
            },
            Event::WindowFocused(true),
        ];
        immediate_wheel(&mut ev, 40.0);
        let steps: Vec<f32> = ev
            .iter()
            .filter_map(|e| match e {
                Event::MouseWheel { unit: MouseWheelUnit::Point, delta, .. } => Some(delta.y),
                _ => None,
            })
            .collect();
        assert!(steps.iter().all(|d| d.abs() < 8.0));
        assert!((steps.iter().sum::<f32>() + 40.0).abs() < 0.01);
        assert!(matches!(ev.last(), Some(Event::WindowFocused(true))));
    }
}
