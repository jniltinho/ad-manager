//! The list table of every page, as the web's .grid-t: one continuous header band (the theme's table-head recipe,
//! semibold labels), rows with a full-width divider, stripes from the second row, the hover tint, numbers aligned
//! right where the web aligns them, the last column stretched. Only the rows on screen are laid out (thousands of
//! users scroll at the cost of a screenful).
//!
//! Cells may be links: the page returns, per cell, a [`Link`] with its own action value `A` (typically the row's
//! action and index). The table reports the clicked link's value in the same frame, so the page resolves it against
//! the row drawn — a link always acts on the account it was drawn for. A link click does not change the selection.

use adm_core::logic::Tone;
use egui::{RichText, Sense, Ui};
use egui_extras::{Column as XColumn, TableBuilder};

use crate::style::Look;

pub struct Column {
    pub title: String,
    pub width: f32,
    /// Its text is drawn as a tag (status, system).
    pub badge: bool,
    /// An action column: its width while the table is narrower than all its columns (then the links show only their
    /// icon).
    pub compact: Option<f32>,
    /// Aligned to the right (numbers, sizes).
    pub right: bool,
    /// Its badge is at least this wide (the Users status: 104).
    pub badge_w: f32,
    /// Its header spans this many columns, centered (the web's "Actions" over the action columns); 0 for a column
    /// covered by an earlier span.
    pub span: usize,
    /// It takes the width left over (the last column when no column says so).
    pub fill: bool,
    /// Empty, the table gives this column all its spare width (the web's max-w-0 cell that absorbs it).
    pub greedy: bool,
    /// Its width also in an empty table (the web's w-* on its heading); otherwise an empty table sizes it by its heading.
    pub fixed: bool,
    /// A badge drawn with the toggle's icon slot also when it is not a link (Users' status).
    pub slot: bool,
}

impl Column {
    pub fn new(title: &str, width: f32) -> Self {
        Self {
            title: title.into(),
            width,
            badge: false,
            compact: None,
            right: false,
            badge_w: 0.0,
            span: 1,
            fill: false,
            greedy: false,
            fixed: false,
            slot: false,
        }
    }

    /// Its badges keep the toggle's icon slot also when they are not links.
    pub fn icon_slot(mut self) -> Self {
        self.slot = true;
        self
    }

    /// As wide also in an empty table (the web's w-* on its heading).
    pub fn fixed(mut self) -> Self {
        self.fixed = true;
        self
    }

    /// The fill column that also takes all the spare width of an empty table (the web's max-w-0 cell).
    pub fn greedy(mut self) -> Self {
        self.fill = true;
        self.greedy = true;
        self
    }

    /// The header spans `n` columns (the next `n - 1` columns get `covered()`).
    pub fn span(mut self, n: usize) -> Self {
        self.span = n;
        self
    }

    /// Takes the width left over.
    pub fn fill(mut self) -> Self {
        self.fill = true;
        self
    }

    /// Under the header of an earlier spanning column.
    pub fn covered(mut self) -> Self {
        self.span = 0;
        self
    }

    pub fn right(mut self) -> Self {
        self.right = true;
        self
    }

    pub fn badge_w(mut self, w: f32) -> Self {
        self.badge = true;
        self.badge_w = w;
        self
    }

    pub fn badge(mut self) -> Self {
        self.badge = true;
        self
    }

    pub fn compact(mut self, w: f32) -> Self {
        self.compact = Some(w);
        self
    }
}

pub struct Link<A> {
    pub icon: &'static str,
    pub hint: String,
    pub action: A,
    /// Keep the column's tag style (unlock beside a system tag).
    pub tag: bool,
}

pub struct Cell<A> {
    pub text: String,
    pub tone: Tone,
    pub link: Option<Link<A>>,
    /// Semibold (a name, as the web's).
    pub bold: bool,
    /// Monospace (a DN, a path, an action id).
    pub mono: bool,
    /// One point smaller (the web's text-[11px] in a 12 px table).
    pub small: bool,
    /// Indented (a tree level) and led by a marker in the accent color (the web's OU tree: ■ at the root, └ below).
    pub indent: f32,
    pub marker: &'static str,
    /// Tags after the text (the web's "container" beside an OU name), a space apart.
    pub tags: Vec<(String, Tone)>,
    /// More text after it in its own tone (the web's muted span after a colored status), cut when it does not fit.
    pub tail: Option<(String, Tone)>,
    /// More links after the first, packed in the same cell (the web's Actions cell).
    pub more: Vec<(String, Tone, Link<A>)>,
    /// A tooltip of a plain cell (the web's title).
    pub tip: String,
    /// The text wraps in the cell (the web's cells without whitespace-nowrap), its lines 1.5 apart.
    pub wrap: bool,
}

impl<A> Cell<A> {
    pub fn new(text: impl Into<String>, tone: Tone) -> Self {
        Self {
            text: text.into(),
            tone,
            link: None,
            bold: false,
            mono: false,
            small: false,
            indent: 0.0,
            marker: "",
            tags: Vec::new(),
            tail: None,
            more: Vec::new(),
            tip: String::new(),
            wrap: false,
        }
    }

    pub fn wrap(mut self) -> Self {
        self.wrap = true;
        self
    }

    pub fn tip(mut self, tip: impl Into<String>) -> Self {
        self.tip = tip.into();
        self
    }

    pub fn tree(mut self, level: usize) -> Self {
        self.indent = 22.0 * level as f32;
        self.marker = if level == 0 { "root" } else { "child" };
        self
    }

    pub fn with_tag(mut self, text: impl Into<String>, tone: Tone) -> Self {
        self.tags.push((text.into(), tone));
        self
    }

    pub fn with_tail(mut self, text: impl Into<String>, tone: Tone) -> Self {
        self.tail = Some((text.into(), tone));
        self
    }

    pub fn bold(mut self) -> Self {
        self.bold = true;
        self
    }

    pub fn small(mut self) -> Self {
        self.small = true;
        self
    }

    pub fn mono(mut self) -> Self {
        self.mono = true;
        self
    }

    pub fn plain(text: impl Into<String>) -> Self {
        Self::new(text, Tone::Normal)
    }

    pub fn link(mut self, icon: &'static str, hint: String, action: A) -> Self {
        self.link = Some(Link { icon, hint, action, tag: false });
        self
    }

    /// One more link in this cell, after the others.
    pub fn and_link(
        mut self,
        text: impl Into<String>,
        tone: Tone,
        icon: &'static str,
        hint: String,
        action: A,
    ) -> Self {
        self.more.push((text.into(), tone, Link { icon, hint, action, tag: false }));
        self
    }
}

#[derive(Debug, Default, Clone)]
pub struct TableState {
    pub sel: Option<usize>,
    scroll_to: Option<usize>,
}

impl TableState {
    pub fn clear(&mut self) {
        self.sel = None;
    }
}

/// The column widths for `avail` points: action columns at their compact width when compact, and the other columns
/// shrunk together (down to 60%) when they still do not fit, so the table never runs past the window.
pub fn fit(cols: &[Column], avail: f32, compact: bool) -> Vec<f32> {
    let mut w: Vec<f32> = cols.iter().map(|c| if compact { c.compact.unwrap_or(c.width) } else { c.width }).collect();
    let fixed: f32 = cols.iter().zip(&w).filter(|(c, _)| compact && c.compact.is_some()).map(|(_, w)| w).sum();
    let flex: f32 = w.iter().sum::<f32>() - fixed;
    if flex > 0.0 && fixed + flex > avail {
        let k = ((avail - fixed) / flex).clamp(0.6, 1.0);
        for (c, x) in cols.iter().zip(w.iter_mut()) {
            if !(compact && c.compact.is_some()) {
                *x = (*x * k).floor();
            }
        }
    }
    w
}

pub struct TableOut<A> {
    /// The link clicked this frame.
    pub clicked: Option<A>,
    /// The row selected by a click this frame (also by the arrow keys).
    pub selected: Option<usize>,
    /// Enter pressed on the selected row.
    pub enter: Option<usize>,
    /// A row double-clicked (opens its details where the page has some).
    pub activated: Option<usize>,
    /// Whether the action columns show only icons.
    pub compact: bool,
}

/// A table to draw: its id, columns and number of rows; `keys` lets it take Up/Down/Enter (no dialog open, no field
/// focused); `row_h` is the web table's own row pitch (0: the text line plus the cell padding and the divider).
pub struct Table<'a> {
    pub id: &'a str,
    pub cols: &'a [Column],
    pub rows: usize,
    pub keys: bool,
    pub row_h: f32,
    /// The 1 px frame of the web's table wrapper (none inside a card, as the web's `border-0`).
    pub frame: bool,
    /// The web wrapper's `max-h-[calc(var(--view-h)-N)]`: at most the window's height less this (0: no cap).
    pub view_minus: f32,
    /// Each row's own height, when they differ (a row with links is taller in the web): `rows` of them.
    pub heights: Option<&'a [f32]>,
}

impl Table<'_> {
    /// Draws the rows on screen; the hint of a hovered link goes to `hover` (the status line).
    pub fn show<A>(
        self,
        ui: &mut Ui,
        look: &Look,
        state: &mut TableState,
        hover: &mut Option<String>,
        cell: impl FnMut(usize, usize) -> Cell<A>,
    ) -> TableOut<A> {
        show(ui, look, &self, state, hover, cell)
    }
}

fn show<A>(
    ui: &mut Ui,
    look: &Look,
    t: &Table<'_>,
    state: &mut TableState,
    hover: &mut Option<String>,
    mut cell: impl FnMut(usize, usize) -> Cell<A>,
) -> TableOut<A> {
    let (id, cols, rows, keys) = (t.id, t.cols, t.rows, t.keys);
    let mut out = TableOut { clicked: None, selected: None, enter: None, activated: None, compact: false };
    if state.sel.is_some_and(|s| s >= rows) {
        state.sel = None;
    }
    if keys && rows > 0 && ui.memory(|m| m.focused().is_none()) {
        let (up, down, enter) = ui.input(|i| {
            (i.key_pressed(egui::Key::ArrowUp), i.key_pressed(egui::Key::ArrowDown), i.key_pressed(egui::Key::Enter))
        });
        if down || up {
            let s = match (state.sel, down) {
                (None, _) => 0,
                (Some(s), true) => (s + 1).min(rows - 1),
                (Some(s), false) => s.saturating_sub(1),
            };
            state.sel = Some(s);
            state.scroll_to = Some(s);
            out.selected = Some(s);
        }
        if enter {
            out.enter = state.sel;
        }
    }
    let text = look.px("control-text", 12.0);
    let (th_px, th_py, td_px, td_py) =
        (look.px("th-px", 8.0), look.px("th-py", 6.0), look.px("td-px", 8.0), look.px("td-py", 4.0));
    // the web's 28-29 points: a line of 1.5, its padding and the divider, plus the point a browser rounds up
    let row_h = if t.row_h > 0.0 { t.row_h } else { (text * 1.5 + 2.0 * td_py + 2.0).round() };
    let head_h = (text * 1.5 + 2.0 * th_py + 1.0).round();
    // the scroll bar's room only when the rows do not fit
    let room = if t.view_minus > 0.0 {
        ui.available_height().min(ui.ctx().content_rect().height() - t.view_minus)
    } else {
        ui.available_height()
    };
    let body_h = t.heights.map_or(rows as f32 * row_h, |h| h.iter().sum());
    let fits = head_h + body_h <= room;
    let avail = ui.available_width() - if fits { 0.0 } else { 14.0 };
    let full: f32 = cols.iter().map(|c| c.width).sum();
    let compact = avail < full;
    out.compact = compact;
    let mut widths = fit(cols, avail, compact);
    if rows == 0 {
        // an empty table as the browser's auto layout: each column as wide as its heading (a spanned title over its
        // columns), the fill column the rest
        let font = egui::FontId::new(text, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into()));
        let mut head_w: Vec<f32> = cols
            .iter()
            .map(|c| {
                if c.fixed {
                    return c.width;
                }
                ui.painter().layout_no_wrap(c.title.clone(), font.clone(), egui::Color32::PLACEHOLDER).size().x
                    + 2.0 * th_px
            })
            .collect();
        for (i, c) in cols.iter().enumerate() {
            if c.span > 1 {
                let share = head_w[i] / c.span as f32;
                for w in head_w.iter_mut().skip(i).take(c.span) {
                    *w = share.max(2.0 * th_px);
                }
            }
        }
        // the spare width shared in proportion to the headings, as table-layout: auto does; a table with no
        // headings to share it (the links column titled by a span) keeps it in its fill column
        let total: f32 = head_w.iter().sum::<f32>().max(1.0);
        let spare = (avail - total).max(0.0);
        if let Some(g) = cols.iter().position(|c| c.greedy) {
            head_w[g] += spare;
            widths = head_w.clone();
        }
        let right_block =
            if cols.iter().any(|c| c.greedy) { 0 } else { cols.iter().rposition(|c| !c.right).map_or(0, |i| i + 1) };
        let shared: f32 = head_w.iter().take(right_block).sum::<f32>().max(1.0);
        for (i, w) in head_w.iter_mut().enumerate() {
            // the numbers at the right end stay as wide as their headings (the web's whitespace-nowrap numbers)
            if i < right_block {
                *w += spare * *w / shared;
            }
        }
        if !cols.iter().any(|c| c.greedy) {
            widths = head_w;
        }
    }
    let (hover_bg, on_head, line, alt) =
        (look.c("row-hover"), look.c("on-table-head"), look.c("line"), look.c("row-alt"));
    let head_grad = look.gradient("table-head");
    let head_fill = look.c("table-head");
    let head_shadow = look.shadows("table-head");
    let framed = ui.push_id(id, |ui| {
        let v = ui.visuals_mut();
        // the web marks the row under the pointer (and here the keyboard's row) with the hover tint, not a selection
        v.selection.bg_fill = hover_bg;
        v.widgets.hovered.weak_bg_fill = hover_bg;
        ui.spacing_mut().item_spacing = egui::vec2(0.0, 0.0);
        let mut tb = TableBuilder::new(ui)
            .striped(false)
            .resizable(false)
            .sense(Sense::click())
            .cell_layout(egui::Layout::left_to_right(egui::Align::Center))
            .min_scrolled_height(80.0)
            .max_scroll_height((room - head_h).max(80.0))
            // a wheel step moves at once: an animation would redraw the whole window for every frame of it
            .animate_scrolling(false)
            // as the web's wrapper: as tall as its rows, at most the room left
            .auto_shrink([false, true])
            .vscroll(!fits);
        if let Some(r) = state.scroll_to.take() {
            tb = tb.scroll_to_row(r, None);
        }
        let n = cols.len();
        let filler = cols.iter().position(|c| c.fill).unwrap_or(n.saturating_sub(1));
        for (i, w) in widths.iter().enumerate() {
            tb = tb.column(if i == filler {
                XColumn::remainder().at_least(*w).clip(true)
            } else {
                XColumn::exact(*w).clip(true)
            });
        }
        // the column that opens the current span, for the title drawn once its last column is painted
        let mut span_from: Option<usize> = None;
        tb.header(head_h, |mut h| {
            for (i, c) in cols.iter().enumerate() {
                if c.span > 1 {
                    span_from = Some(i);
                }
                h.col(|ui| {
                    let rect = ui.max_rect();
                    crate::depth::paint_box(ui.painter(), rect, head_grad, head_fill, head_shadow);
                    ui.painter().hline(rect.x_range(), rect.bottom() - 0.5, egui::Stroke::new(1.0, line));
                    let last_of_span = span_from.is_some_and(|f| i + 1 == f + cols[f].span);
                    if let (true, Some(f)) = (last_of_span, span_from) {
                        // centered over the columns it spans, after their backgrounds
                        let w: f32 = widths.iter().skip(f).take(cols[f].span).sum();
                        let g = ui.painter().layout_no_wrap(
                            cols[f].title.clone(),
                            egui::FontId::new(text, egui::FontFamily::Name(crate::fonts::SEMIBOLD.into())),
                            on_head,
                        );
                        let left = rect.right() - w;
                        let at = egui::pos2(left + (w - g.size().x) / 2.0, rect.center().y - g.size().y / 2.0);
                        let clip = egui::Rect::from_min_max(egui::pos2(left, rect.top()), rect.max);
                        // the layer's painter: a cell's painter only clips narrower (it intersects)
                        ui.ctx()
                            .layer_painter(ui.layer_id())
                            .with_clip_rect(clip.intersect(ui.clip_rect().with_min_x(left)))
                            .galley(at, g, on_head);
                    } else if c.span == 1 {
                        // a heading is not cut by the padding after it (the web's th does not truncate)
                        pad(ui, c.right, th_px, th_px / 2.0, |ui| {
                            ui.label(crate::style::semibold(&c.title, text).color(on_head));
                        });
                    }
                });
            }
        })
        .body(|body| {
            let add_row = |mut row: egui_extras::TableRow<'_, '_>| {
                let r = row.index();
                let selected = state.sel == Some(r);
                row.set_selected(selected);
                for (ci, c) in cols.iter().enumerate() {
                    let cl = cell(r, ci);
                    row.col(|ui| {
                        let rect = ui.max_rect();
                        if r % 2 == 1 && !selected {
                            ui.painter().rect_filled(rect, 0.0, alt);
                        }
                        // the first row's top is the head's bottom line (the web's collapsed border: one line)
                        if r > 0 {
                            ui.painter().hline(rect.x_range(), rect.top() + 0.5, egui::Stroke::new(1.0, line));
                        }
                        // the web's padding after a text too (it truncates there); the links' columns keep half of it,
                        // their widths are each link and the web's gap after it
                        let tail = if c.compact.is_some() { td_px / 2.0 } else { td_px };
                        pad(ui, c.right, td_px, tail, |ui| draw_cell(ui, look, c, cl, compact, hover, &mut out));
                    });
                }
                let resp = row.response();
                if resp.clicked() {
                    state.sel = Some(r);
                    out.selected = Some(r);
                }
                if resp.double_clicked() {
                    out.activated = Some(r);
                }
            };
            match t.heights {
                Some(h) => body.heterogeneous_rows(h.iter().copied(), add_row),
                None => body.rows(row_h, rows, add_row),
            }
        });
    });
    // the header band over the scroll bar's column too (the web's sticky head spans the wrapper)
    if !fits {
        // the 14 points kept for the bar (`avail`): the fill column grows past its width, so the columns' sum is not
        // where the heads end
        let rect = framed.response.rect;
        let gutter = egui::Rect::from_min_max(
            egui::pos2((rect.right() - 14.0).max(rect.left()), rect.top()),
            egui::pos2(rect.right(), rect.top() + head_h),
        );
        if gutter.width() > 0.0 {
            // without the outer layers: under the heads the rows cover the band's drop shadow, here nothing would
            // (a stray dash under the band, above the bar)
            let inner: Vec<_> = head_shadow.iter().copied().filter(|l| l.inset).collect();
            crate::depth::paint_box(ui.painter(), gutter, head_grad, head_fill, &inner);
            ui.painter().hline(gutter.x_range(), gutter.bottom() - 0.5, egui::Stroke::new(1.0, line));
        }
    }
    // the table's 1 px frame, as the web's wrapper
    if t.frame {
        ui.painter().rect_stroke(framed.response.rect, 0.0, egui::Stroke::new(1.0, line), egui::StrokeKind::Inside);
    }
    out
}

/// The web's empty-table row (`py-3 text-center text-fg-muted` across the columns): 42 points with the table's line
/// under it.
pub fn empty_row(ui: &mut Ui, look: &crate::style::Look, text: &str) {
    let muted = look.c("fg-muted");
    let (row, _) = ui.allocate_exact_size(egui::vec2(ui.available_width(), 42.0), Sense::hover());
    let g = ui.painter().layout_no_wrap(text.to_string(), egui::FontId::proportional(12.0), muted);
    ui.painter().galley(row.center() - g.size() / 2.0, g, muted);
    ui.painter().hline(row.x_range(), row.bottom() - 0.5, egui::Stroke::new(1.0, look.c("line")));
}

/// The web's row heights at 1280 (Chrome's layout of a 12-point line, its padding and the collapsed border): a row with
/// action links (inline-flex buttons) and one of text and badges only.
pub const ROW_LINKS: f32 = 28.76;

/// A text-only row: Chrome rounds its line boxes per device pixel, so it is 26.94 at 1x and 27.76 at 2x.
pub fn row_text(ctx: &egui::Context) -> f32 {
    if ctx.pixels_per_point() >= 1.5 { 27.76 } else { 26.94 }
}

/// Draws `add` with the cell's side padding, from the left or (numbers) from the right.
fn pad(ui: &mut Ui, right: bool, px: f32, tail: f32, add: impl FnOnce(&mut Ui)) {
    let layout = if right {
        egui::Layout::right_to_left(egui::Align::Center)
    } else {
        egui::Layout::left_to_right(egui::Align::Center)
    };
    ui.with_layout(layout, |ui| {
        let clip = ui.max_rect().shrink2(egui::vec2(px / 2.0, 0.0)).intersect(ui.clip_rect());
        // the padding before the text, `tail` after it: a truncated text ends in its ellipsis there
        let m = ui.max_rect();
        let inner = if right {
            egui::Rect::from_min_max(egui::pos2(m.left() + tail, m.top()), egui::pos2(m.right() - px, m.bottom()))
        } else {
            egui::Rect::from_min_max(egui::pos2(m.left() + px, m.top()), egui::pos2(m.right() - tail, m.bottom()))
        };
        ui.scope_builder(egui::UiBuilder::new().max_rect(inner).layout(layout), |ui| {
            ui.set_clip_rect(clip);
            add(ui);
        });
    });
}

/// One body cell: a link (text or icon, or a badge that is a link), a badge, or text — semibold, monospace or in its
/// tone's ink.
fn draw_cell<A>(
    ui: &mut Ui,
    look: &Look,
    c: &Column,
    cl: Cell<A>,
    compact: bool,
    hover: &mut Option<String>,
    out: &mut TableOut<A>,
) {
    let size = look.px("control-text", 12.0);
    // a link is its text alone, as the web's .act (egui's button padding made the gaps wider); a right-aligned
    // cell keeps the .act's px-1 at its end
    ui.spacing_mut().button_padding.x = 0.0;
    if c.right && (cl.link.is_some() || !cl.more.is_empty()) {
        ui.add_space(4.0);
    }
    let mut ink = look.tone(cl.tone);
    let rich = |s: String, ink: egui::Color32| {
        let size = if cl.small { size - 1.0 } else { size };
        let r = if cl.bold { crate::style::semibold(s, size) } else { RichText::new(s).size(size) };
        if cl.mono { r.family(egui::FontFamily::Monospace).size(size - 1.0) } else { r }.color(ink)
    };
    if !cl.more.is_empty() {
        let icon_only = compact && c.compact.is_some();
        let mut all: Vec<(String, Tone, Link<A>)> =
            cl.link.into_iter().map(|l| (cl.text.clone(), cl.tone, l)).chain(cl.more).collect();
        if c.right {
            all.reverse();
        }
        ui.spacing_mut().item_spacing.x = 8.0;
        for (text, tone, l) in all {
            let ink = if tone == Tone::Normal { look.c("link") } else { look.tone(tone) };
            let s =
                if icon_only { l.icon.to_string() } else { format!("{}\u{2009}{}", l.icon, text).trim().to_string() };
            let resp = ui.add(egui::Button::new(rich(s, ink)).frame(false));
            link_done(ui, resp, l, hover, out);
        }
        return;
    }
    match cl.link {
        Some(l) => {
            if cl.tone == Tone::Normal && !l.tag {
                ink = look.c("link");
            }
            let icon_only = compact && c.compact.is_some() && !l.tag;
            let text = match (l.icon.is_empty(), icon_only || cl.text.is_empty()) {
                (true, _) => cl.text.clone(),
                (false, true) => l.icon.to_string(),
                (false, false) => format!("{}\u{2009}{}", l.icon, cl.text), // the web's gap-0.5
            };
            let resp = if c.badge && !cl.text.is_empty() && (l.icon.is_empty() || l.tag) {
                if l.tag && !l.icon.is_empty() {
                    let ir = ui.add(egui::Button::new(RichText::new(l.icon).color(look.c("link"))).frame(false));
                    let _ = crate::widgets::tag_w(ui, look, &cl.text, cl.tone, c.badge_w);
                    ir
                } else {
                    crate::widgets::tag_button(ui, look, &cl.text, cl.tone)
                }
            } else {
                // an action column's links are never cut (the web's .act links overflow their cell)
                let shown = if c.compact.is_some() {
                    text.clone()
                } else {
                    elide(ui, &text, &|s| rich(s, ink), ui.available_width())
                };
                let r = ui.add(egui::Button::new(rich(shown.clone(), ink)).frame(false));
                if shown != text {
                    let _ = ui.ctx().accesskit_node_builder(r.id, |n| n.set_label(text.clone()));
                }
                r
            };
            link_done(ui, resp, l, hover, out);
            for (text, tone) in &cl.tags {
                ui.add_space(6.0);
                let _ = crate::widgets::tag(ui, look, text, *tone);
            }
        }
        None if c.badge && !cl.text.is_empty() && c.slot => {
            let _ = crate::widgets::tag_slot(ui, look, &cl.text, cl.tone);
        }
        None if c.badge && !cl.text.is_empty() => {
            let _ = crate::widgets::tag_w(ui, look, &cl.text, cl.tone, c.badge_w);
        }
        None => {
            if cl.indent > 0.0 {
                ui.add_space(cl.indent);
            }
            if !cl.marker.is_empty() {
                // drawn, not typed: egui's fonts have no box-drawing glyphs (the web's ■ and └)
                let (r, _) = ui.allocate_exact_size(egui::vec2(10.0, size), Sense::hover());
                let accent = look.c("accent");
                if cl.marker == "root" {
                    ui.painter().rect_filled(
                        egui::Rect::from_center_size(r.center(), egui::vec2(6.0, 6.0)),
                        0.0,
                        accent,
                    );
                } else {
                    let s = egui::Stroke::new(1.0, accent);
                    ui.painter().vline(r.left() + 2.0, r.top()..=r.center().y, s);
                    ui.painter().hline(r.left() + 2.0..=r.right(), r.center().y, s);
                }
                ui.add_space(4.0);
            }
            // a wrapping text that fits is drawn as any other (centered in its row, as the web's align-middle)
            let fits = cl.wrap && {
                let g = egui::WidgetText::from(rich(cl.text.clone(), ink)).into_galley(
                    ui,
                    Some(egui::TextWrapMode::Extend),
                    f32::INFINITY,
                    egui::TextStyle::Body,
                );
                g.size().x <= ui.available_width()
            };
            if cl.wrap && !fits {
                let w = ui.available_width();
                ui.vertical(|ui| {
                    ui.set_max_width(w);
                    ui.add(
                        egui::Label::new(rich(cl.text.clone(), ink).line_height(Some(size * 1.5)))
                            .wrap()
                            .selectable(false),
                    );
                });
                return;
            }
            if let Some((tail, tone)) = &cl.tail {
                ui.spacing_mut().item_spacing.x = 0.0;
                ui.add(egui::Label::new(rich(cl.text.clone(), ink)).selectable(false));
                let tink = look.tone(*tone);
                let shown = elide(ui, tail, &|s| rich(s, tink), ui.available_width());
                let r = ui.add(egui::Label::new(rich(shown.clone(), tink)).selectable(false));
                if shown != *tail {
                    // the whole text for screen readers
                    let _ = ui.ctx().accesskit_node_builder(r.id, |n| n.set_label(tail.clone()));
                }
                if !cl.tip.is_empty() {
                    let _ = r.on_hover_text(&cl.tip);
                } else if shown != *tail {
                    let _ = r.on_hover_text(format!("{}{tail}", cl.text));
                }
                return;
            }
            let shown = elide(ui, &cl.text, &|s| rich(s, ink), ui.available_width());
            let r = ui.add(egui::Label::new(rich(shown.clone(), ink)).selectable(false));
            if shown != cl.text {
                // the whole text for screen readers and on hover (the web's title)
                let _ = ui.ctx().accesskit_node_builder(r.id, |n| n.set_label(cl.text.clone()));
            }
            if !cl.tip.is_empty() {
                let _ = r.on_hover_text(&cl.tip);
            } else if shown != cl.text {
                let _ = r.on_hover_text(&cl.text);
            }
            for (i, (text, tone)) in cl.tags.iter().enumerate() {
                // after a text the web's tag is an inline element: one space of the cell's font from it (a tag alone
                // starts at the cell's padding)
                // tags side by side touch (Vue drops the line break between them)
                if i == 0 && !cl.text.is_empty() {
                    ui.add_space(ui.ctx().fonts_mut(|f| f.glyph_width(&egui::FontId::proportional(size), ' ')));
                }
                let _ = if cl.mono {
                    crate::widgets::tag_mono(ui, look, text, *tone)
                } else {
                    crate::widgets::tag(ui, look, text, *tone)
                };
            }
        }
    }
}

/// `text` cut as the browser's text-overflow: ellipsis cuts it: the longest start whose right edge plus the "…" fits
/// in `max_w` (egui's own truncation keeps a glyph whose left edge fits: one more).
fn elide(ui: &Ui, text: &str, make: &dyn Fn(String) -> RichText, max_w: f32) -> String {
    let w = |s: &str| {
        egui::WidgetText::from(make(s.to_string()))
            .into_galley(ui, Some(egui::TextWrapMode::Extend), f32::INFINITY, egui::TextStyle::Body)
            .size()
            .x
    };
    if w(text) <= max_w + 0.01 {
        return text.to_string();
    }
    let room = max_w - w("…");
    let ends: Vec<usize> = text.char_indices().map(|(i, _)| i).skip(1).chain(std::iter::once(text.len())).collect();
    // the widths grow with the prefix: the last end that fits
    let n = ends.partition_point(|&e| w(&text[..e]) <= room + 0.01);
    let cut = if n == 0 { "" } else { &text[..ends[n - 1]] };
    format!("{cut}…")
}

/// A drawn link: the hand cursor, its accessible name (what it does, also when only the icon is drawn), the hint on
/// hover and the action on click.
fn link_done<A>(ui: &Ui, resp: egui::Response, l: Link<A>, hover: &mut Option<String>, out: &mut TableOut<A>) {
    let resp = resp.on_hover_cursor(egui::CursorIcon::PointingHand);
    if !l.hint.is_empty() {
        let _ = ui.ctx().accesskit_node_builder(resp.id, |n| n.set_label(l.hint.clone()));
    }
    if resp.hovered() {
        *hover = Some(l.hint.clone());
    }
    if resp.clicked() {
        out.clicked = Some(l.action);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn elides_as_the_browser() {
        let mut h = egui_kittest::Harness::new_ui(|ui| {
            let make = |s: String| RichText::new(s).size(12.0);
            let w = |s: &str| {
                egui::WidgetText::from(make(s.into()))
                    .into_galley(ui, Some(egui::TextWrapMode::Extend), f32::INFINITY, egui::TextStyle::Body)
                    .size()
                    .x
            };
            assert_eq!(elide(ui, "abcdef", &make, w("abcdef")), "abcdef", "fits");
            // "abcd…" is half a point too wide: the browser shows "abc…" (egui's own keeps the d, its left edge fits)
            assert_eq!(elide(ui, "abcdefghijkl", &make, w("abcd") + w("…") - 0.5), "abc…");
            assert_eq!(
                elide(ui, "ab defghijkl", &make, w("ab d") + w("…") - 0.5),
                "ab …",
                "the space kept, as the browser"
            );
        });
        h.run();
    }

    #[test]
    fn widths_fit() {
        let cols = [Column::new("a", 200.0), Column::new("b", 200.0), Column::new("", 100.0).compact(30.0)];
        assert_eq!(fit(&cols, 600.0, false), [200.0, 200.0, 100.0]);
        assert_eq!(fit(&cols, 430.0, true), [200.0, 200.0, 30.0]);
        assert_eq!(fit(&cols, 230.0, true), [120.0, 120.0, 30.0], "shrunk to 60% at most");
    }
}

#[cfg(test)]
mod ui_tests {
    use super::*;
    use egui_kittest::Harness;
    use egui_kittest::kittest::Queryable;

    #[test]
    fn click_selects_and_link_does_not() {
        let look = crate::style::Look::new("classic-blue").unwrap();
        let mut h = Harness::builder().with_size(egui::vec2(600.0, 300.0)).build_ui_state(
            move |ui, st: &mut (TableState, Option<usize>)| {
                // the theme's fonts (the semibold family of the header), as the app installs them at start
                if !ui.ctx().fonts(|f| f.families().contains(&egui::FontFamily::Name(crate::fonts::SEMIBOLD.into()))) {
                    look.apply(ui.ctx()); // in force from the next frame
                    return;
                }
                let cols = [Column::new("Name", 200.0), Column::new("", 100.0)];
                let mut hover = None;
                let out = Table {
                    id: "t",
                    cols: &cols,
                    rows: 3,
                    keys: true,
                    row_h: 0.0,
                    frame: true,
                    view_minus: 0.0,
                    heights: None,
                }
                .show(ui, &look, &mut st.0, &mut hover, |r, c| {
                    if c == 0 { Cell::plain(format!("row {r}")) } else { Cell::plain("go").link("", String::new(), r) }
                });
                if let Some(r) = out.clicked {
                    st.1 = Some(r);
                }
            },
            (TableState::default(), None),
        );
        h.step();
        h.get_by_label("row 1").click();
        h.run_steps(2);
        assert_eq!(h.state().0.sel, Some(1), "a plain cell selects its row");
        h.get_all_by_label("go").nth(2).unwrap().click();
        h.run_steps(2);
        assert_eq!(h.state().1, Some(2), "the link reports its own row");
        assert_eq!(h.state().0.sel, Some(1), "a link leaves the selection");
    }
}
