//! The list table of every page, styled like the web grid: the theme's header, zebra rows, the whole selected row
//! highlighted, the last column stretched. Only the rows on screen are laid out (thousands of users scroll at the
//! cost of a screenful).
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
}

impl Column {
    pub fn new(title: &str, width: f32) -> Self {
        Self { title: title.into(), width, badge: false, compact: None }
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
}

impl<A> Cell<A> {
    pub fn new(text: impl Into<String>, tone: Tone) -> Self {
        Self { text: text.into(), tone, link: None }
    }

    pub fn plain(text: impl Into<String>) -> Self {
        Self::new(text, Tone::Normal)
    }

    pub fn link(mut self, icon: &'static str, hint: String, action: A) -> Self {
        self.link = Some(Link { icon, hint, action, tag: false });
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

const ROW_H: f32 = 26.0;

/// A table to draw: its id, columns and number of rows; `keys` lets it take Up/Down/Enter (no dialog open, no field
/// focused).
pub struct Table<'a> {
    pub id: &'a str,
    pub cols: &'a [Column],
    pub rows: usize,
    pub keys: bool,
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
    let gap = 4.0;
    let avail = ui.available_width() - gap * cols.len() as f32 - 14.0; // the scroll bar's room
    let full: f32 = cols.iter().map(|c| c.width).sum();
    let compact = avail < full;
    out.compact = compact;
    let widths = fit(cols, avail, compact);
    let (sel_bg, on_sel, head, on_head, line) =
        (look.c("selection"), look.c("on-selection"), look.c("table-head"), look.c("on-table-head"), look.c("line"));
    ui.push_id(id, |ui| {
        let v = ui.visuals_mut();
        v.selection.bg_fill = sel_bg;
        v.faint_bg_color = look.c("row-alt");
        v.widgets.hovered.weak_bg_fill = look.c("row-hover");
        ui.spacing_mut().item_spacing.x = gap;
        let mut tb = TableBuilder::new(ui)
            .striped(true)
            .resizable(false)
            .sense(Sense::click())
            .cell_layout(egui::Layout::left_to_right(egui::Align::Center))
            .min_scrolled_height(80.0)
            // a wheel step moves at once: an animation would redraw the whole window for every frame of it
            .animate_scrolling(false)
            .auto_shrink([false, false]);
        if let Some(r) = state.scroll_to.take() {
            tb = tb.scroll_to_row(r, None);
        }
        let n = cols.len();
        for (i, w) in widths.iter().enumerate() {
            tb = tb.column(if i + 1 == n { XColumn::remainder().at_least(*w) } else { XColumn::exact(*w).clip(true) });
        }
        tb.header(26.0, |mut h| {
            for c in cols {
                h.col(|ui| {
                    ui.painter().rect_filled(ui.max_rect().expand2(egui::vec2(4.0, 0.0)), 0.0, head);
                    ui.label(RichText::new(&c.title).strong().size(12.0).color(on_head));
                });
            }
        })
        .body(|body| {
            body.rows(ROW_H, rows, |mut row| {
                let r = row.index();
                let selected = state.sel == Some(r);
                row.set_selected(selected);
                for (ci, c) in cols.iter().enumerate() {
                    let cl = cell(r, ci);
                    row.col(|ui| {
                        let rect = ui.max_rect();
                        ui.painter().hline(
                            rect.x_range().expand(4.0),
                            rect.bottom() + 1.5,
                            egui::Stroke::new(1.0, line.gamma_multiply(0.5)),
                        );
                        let mut ink = if selected { on_sel } else { look.tone(cl.tone) };
                        match cl.link {
                            Some(l) => {
                                if cl.tone == Tone::Normal && !l.tag && !selected {
                                    ink = look.c("link");
                                }
                                let icon_only = compact && c.compact.is_some() && !l.tag;
                                let text = match (l.icon.is_empty(), icon_only || cl.text.is_empty()) {
                                    (true, _) => cl.text.clone(),
                                    (false, true) => l.icon.to_string(),
                                    (false, false) => format!("{} {}", l.icon, cl.text),
                                };
                                let resp = if c.badge && !cl.text.is_empty() && (l.icon.is_empty() || l.tag) {
                                    if l.tag && !l.icon.is_empty() {
                                        let ir = ui.add(
                                            egui::Button::new(RichText::new(l.icon).color(look.c("link"))).frame(false),
                                        );
                                        let _ = crate::widgets::tag(ui, look, &cl.text, cl.tone);
                                        ir
                                    } else {
                                        crate::widgets::tag_button(ui, look, &cl.text, cl.tone)
                                    }
                                } else {
                                    ui.add(egui::Button::new(RichText::new(text).color(ink)).frame(false).truncate())
                                };
                                let resp = resp.on_hover_cursor(egui::CursorIcon::PointingHand);
                                // the accessible name says what it does, also when only the icon is drawn
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
                            None if c.badge && !cl.text.is_empty() => {
                                let _ = crate::widgets::tag(ui, look, &cl.text, cl.tone);
                            }
                            None => {
                                ui.add(
                                    egui::Label::new(RichText::new(&cl.text).color(ink)).truncate().selectable(false),
                                );
                            }
                        }
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
            });
        });
    });
    out
}

#[cfg(test)]
mod tests {
    use super::*;

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
                let cols = [Column::new("Name", 200.0), Column::new("", 100.0)];
                let mut hover = None;
                let out = Table { id: "t", cols: &cols, rows: 3, keys: true }.show(
                    ui,
                    &look,
                    &mut st.0,
                    &mut hover,
                    |r, c| {
                        if c == 0 {
                            Cell::plain(format!("row {r}"))
                        } else {
                            Cell::plain("go").link("", String::new(), r)
                        }
                    },
                );
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
