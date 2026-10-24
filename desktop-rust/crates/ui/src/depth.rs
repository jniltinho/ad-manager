//! The web's depth on egui (design D5): a part's background gradient and its box-shadow layers, from the theme's
//! recipe (`adm_core::theme`). Gradients follow their CSS direction and interpolate in their CSS space (Oklab
//! precomputed into extra stops, so the colors match the browser's). Shadows keep CSS order — the first listed layer is
//! on top — so they are painted back to front: outer layers below the background, inset ones above it.

use adm_core::theme::{Gradient, Layer, Rgba};
use egui::epaint::{Mesh, Shape, Vertex, WHITE_UV};
use egui::{Color32, CornerRadius, Painter, Pos2, Rect, Vec2};

use crate::style::c32;

fn lin(c: u8) -> f32 {
    let v = f32::from(c) / 255.0;
    if v <= 0.04045 { v / 12.92 } else { ((v + 0.055) / 1.055).powf(2.4) }
}

fn unlin(x: f32) -> u8 {
    let v = if x <= 0.003_130_8 { x * 12.92 } else { 1.055 * x.max(0.0).powf(1.0 / 2.4) - 0.055 };
    (v * 255.0).round().clamp(0.0, 255.0) as u8
}

/// sRGB → Oklab (L, a, b) and alpha.
fn to_oklab(c: Rgba) -> [f32; 4] {
    let [r, g, b, a] = c.0;
    let (r, g, b) = (lin(r), lin(g), lin(b));
    let l = (0.412_221_46 * r + 0.536_332_55 * g + 0.051_445_995 * b).cbrt();
    let m = (0.211_903_5 * r + 0.680_699_5 * g + 0.107_396_96 * b).cbrt();
    let s = (0.088_302_46 * r + 0.281_718_85 * g + 0.629_978_7 * b).cbrt();
    [
        0.210_454_26 * l + 0.793_617_8 * m - 0.004_072_047 * s,
        1.977_998_5 * l - 2.428_592_2 * m + 0.450_593_7 * s,
        0.025_904_037 * l + 0.782_771_77 * m - 0.808_675_77 * s,
        f32::from(a),
    ]
}

fn from_oklab(o: [f32; 4]) -> Rgba {
    let l = (o[0] + 0.396_337_78 * o[1] + 0.215_803_76 * o[2]).powi(3);
    let m = (o[0] - 0.105_561_346 * o[1] - 0.063_854_17 * o[2]).powi(3);
    let s = (o[0] - 0.089_484_18 * o[1] - 1.291_485_5 * o[2]).powi(3);
    Rgba([
        unlin(4.076_741_7 * l - 3.307_711_6 * m + 0.230_969_94 * s),
        unlin(-1.268_438 * l + 2.609_757_4 * m - 0.341_319_38 * s),
        unlin(-0.004_196_086_3 * l - 0.703_418_6 * m + 1.707_614_7 * s),
        o[3].round().clamp(0.0, 255.0) as u8,
    ])
}

/// The color of `g` at `t` (0–1), interpolated in its space.
pub fn color_at(g: &Gradient, t: f32) -> Rgba {
    let stops = &g.stops;
    let Some(first) = stops.first() else { return Rgba::default() };
    if t <= first.pos {
        return first.color;
    }
    for w in stops.windows(2) {
        let (a, b) = (w[0], w[1]);
        if t <= b.pos {
            let k = if b.pos > a.pos { (t - a.pos) / (b.pos - a.pos) } else { 1.0 };
            return if g.space == "oklab" {
                let (x, y) = (to_oklab(a.color), to_oklab(b.color));
                from_oklab([0, 1, 2, 3].map(|i| x[i] + (y[i] - x[i]) * k))
            } else {
                let mix = |i: usize| {
                    (f32::from(a.color.0[i]) + (f32::from(b.color.0[i]) - f32::from(a.color.0[i])) * k).round() as u8
                };
                Rgba([mix(0), mix(1), mix(2), mix(3)])
            };
        }
    }
    stops.last().map_or(first.color, |s| s.color)
}

/// The unit vector a CSS direction points to (screen coordinates: y down).
fn axis(dir: &str) -> Vec2 {
    match dir {
        "to bottom" => Vec2::new(0.0, 1.0),
        "to top" => Vec2::new(0.0, -1.0),
        "to right" => Vec2::new(1.0, 0.0),
        "to left" => Vec2::new(-1.0, 0.0),
        d => {
            // CSS angles: 0deg points up, clockwise
            let deg: f32 = d.trim_end_matches("deg").trim().parse().unwrap_or(180.0);
            let r = deg.to_radians();
            Vec2::new(r.sin(), -r.cos())
        }
    }
}

/// A mesh filling `rect` with `g`: bands across the gradient's axis, 12 per stop interval, so an Oklab interpolation is
/// followed closely.
pub fn gradient_mesh(rect: Rect, g: &Gradient) -> Mesh {
    let mut mesh = Mesh::default();
    let ax = axis(&g.dir);
    // the gradient line's length for this box (CSS: the corners touch the 0% and 100% lines)
    let half = (rect.width() * ax.x.abs() + rect.height() * ax.y.abs()) / 2.0;
    let center = rect.center();
    let mut ts: Vec<f32> = Vec::new();
    for w in g.stops.windows(2) {
        for i in 0..12 {
            ts.push(w[0].pos + (w[1].pos - w[0].pos) * i as f32 / 12.0);
        }
    }
    ts.push(g.stops.last().map_or(1.0, |s| s.pos));
    ts.insert(0, 0.0);
    ts.push(1.0);
    ts.sort_by(f32::total_cmp);
    ts.dedup_by(|a, b| (*a - *b).abs() < 1e-4);
    // the bands are drawn in the axis' frame: a quad per step, clipped to the rect by construction for the 4 sides,
    // and approximated by the box for angles (the painter clips to the widget)
    let perp = Vec2::new(-ax.y, ax.x);
    let span = rect.width() * perp.x.abs() + rect.height() * perp.y.abs();
    let pos = |t: f32, s: f32| -> Pos2 { center + ax * (-half + 2.0 * half * t) + perp * s };
    for w in ts.windows(2) {
        let (t0, t1) = (w[0], w[1]);
        let (c0, c1) = (c32(color_at(g, t0)), c32(color_at(g, t1)));
        let i = mesh.vertices.len() as u32;
        for (p, c) in [
            (pos(t0, -span / 2.0), c0),
            (pos(t0, span / 2.0), c0),
            (pos(t1, span / 2.0), c1),
            (pos(t1, -span / 2.0), c1),
        ] {
            mesh.vertices.push(Vertex { pos: p, uv: WHITE_UV, color: c });
        }
        mesh.indices.extend_from_slice(&[i, i + 1, i + 2, i, i + 2, i + 3]);
    }
    mesh
}

/// An outer box-shadow layer as an egui shadow shape.
fn outer(rect: Rect, l: &Layer) -> Shape {
    let clamp8 = |v: f32| v.round().clamp(-128.0, 127.0) as i8;
    let clampu = |v: f32| v.round().clamp(0.0, 255.0) as u8;
    let s = egui::Shadow {
        offset: [clamp8(l.dx), clamp8(l.dy)],
        blur: clampu(l.blur),
        spread: clampu(l.spread),
        color: c32(l.color),
    };
    s.as_shape(rect, CornerRadius::ZERO).into()
}

/// An inset layer as bands inside `rect`: the edge its offset points from (dx > 0 left, < 0 right; dy > 0 top, < 0
/// bottom), the spread all around, and a blur as a band fading inwards.
fn inset(rect: Rect, l: &Layer, out: &mut Vec<Shape>) {
    let c = c32(l.color);
    let mut band = |r: Rect, col: Color32| out.push(Shape::rect_filled(r.intersect(rect), 0.0, col));
    let (dx, dy, sp, blur) = (l.dx, l.dy, l.spread.max(0.0), l.blur.max(0.0));
    let w = |off: f32| off.abs() + sp;
    let mut edge = |r: Rect, inward: Vec2| {
        if blur > 0.0 {
            // a blurred inset fades over its blur width: strips of falling alpha
            let n = (blur.ceil() as usize).clamp(1, 12);
            let step = blur / n as f32;
            for i in 0..n {
                band(r.translate(inward * step * i as f32), c.gamma_multiply(1.0 - i as f32 / n as f32));
            }
        } else {
            band(r, c);
        }
    };
    let (wl, wr) = (if dx > 0.0 { w(dx) } else { sp }, if dx < 0.0 { w(dx) } else { sp });
    let (wt, wb) = (if dy > 0.0 { w(dy) } else { sp }, if dy < 0.0 { w(dy) } else { sp });
    if wl > 0.0 {
        edge(Rect::from_min_size(rect.min, Vec2::new(wl.min(rect.width()), rect.height())), Vec2::X);
    }
    if wr > 0.0 {
        edge(
            Rect::from_min_size(Pos2::new(rect.max.x - wr, rect.min.y), Vec2::new(wr.min(rect.width()), rect.height())),
            -Vec2::X,
        );
    }
    if wt > 0.0 {
        edge(Rect::from_min_size(rect.min, Vec2::new(rect.width(), wt.min(rect.height()))), Vec2::Y);
    }
    if wb > 0.0 {
        edge(
            Rect::from_min_size(Pos2::new(rect.min.x, rect.max.y - wb), Vec2::new(rect.width(), wb.min(rect.height()))),
            -Vec2::Y,
        );
    }
}

/// The shapes of a repeated background over `rect`, its tiles of 22 px from the rect's top left as the web's
/// `background-size: 22px 22px`: a dot at each tile's center, or lines every `period` px.
pub fn pattern_shapes(rect: Rect, p: &adm_core::theme::Pattern) -> Vec<Shape> {
    use adm_core::theme::PatternKind;
    const TILE: f32 = 22.0;
    let color = Color32::from_rgba_unmultiplied(p.color.0[0], p.color.0[1], p.color.0[2], p.color.0[3]);
    let mut out = Vec::new();
    match p.kind {
        PatternKind::Dots => {
            let mut y = rect.top() + TILE / 2.0;
            while y < rect.bottom() {
                let mut x = rect.left() + TILE / 2.0;
                while x < rect.right() {
                    let c = egui::pos2(x, y);
                    out.push(if p.w == p.h {
                        Shape::circle_filled(c, p.w, color)
                    } else {
                        Shape::rect_filled(Rect::from_center_size(c, egui::vec2(2.0 * p.w, 2.0 * p.h)), p.w, color)
                    });
                    x += TILE;
                }
                y += TILE;
            }
        }
        PatternKind::Lines if p.period > 0.0 => {
            let mut y = rect.top();
            while y < rect.bottom() {
                out.push(Shape::rect_filled(
                    Rect::from_min_max(
                        egui::pos2(rect.left(), y),
                        egui::pos2(rect.right(), (y + p.h).min(rect.bottom())),
                    ),
                    0.0,
                    color,
                ));
                y += p.period;
            }
        }
        PatternKind::Lines => {}
    }
    out
}

/// The shapes of a part's box: its outer shadows, its background (the gradient, or `fill`), then its inset layers;
/// every list back to front so the first CSS layer ends on top.
pub fn box_shapes(rect: Rect, gradient: Option<&Gradient>, fill: Color32, shadows: &[Layer]) -> Vec<Shape> {
    let mut out = Vec::new();
    for l in shadows.iter().rev().filter(|l| !l.inset) {
        out.push(outer(rect, l));
    }
    match gradient {
        Some(g) => out.push(Shape::mesh(gradient_mesh(rect, g))),
        None => out.push(Shape::rect_filled(rect, 0.0, fill)),
    }
    for l in shadows.iter().rev().filter(|l| l.inset) {
        inset(rect, l, &mut out);
    }
    out
}

/// Multiplies a shape's colors by `k` (a disabled control at the web's 50% opacity).
pub fn fade(s: &mut Shape, k: f32) {
    match s {
        Shape::Rect(r) => {
            r.fill = r.fill.gamma_multiply(k);
            r.stroke.color = r.stroke.color.gamma_multiply(k);
        }
        Shape::Mesh(m) => {
            for v in &mut std::sync::Arc::make_mut(m).vertices {
                v.color = v.color.gamma_multiply(k);
            }
        }
        Shape::Vec(v) => v.iter_mut().for_each(|s| fade(s, k)),
        _ => {}
    }
}

/// Paints a part's box now (see [`box_shapes`]).
pub fn paint_box(painter: &Painter, rect: Rect, gradient: Option<&Gradient>, fill: Color32, shadows: &[Layer]) {
    painter.extend(box_shapes(rect, gradient, fill, shadows));
}

#[cfg(test)]
mod tests {
    #![allow(clippy::unwrap_used)]
    use super::*;
    use adm_core::theme::Stop;

    fn g(dir: &str, space: &str, a: [u8; 4], b: [u8; 4]) -> Gradient {
        Gradient {
            dir: dir.into(),
            space: space.into(),
            stops: vec![Stop { color: Rgba(a), pos: 0.0 }, Stop { color: Rgba(b), pos: 1.0 }],
        }
    }

    #[test]
    fn oklab_midpoint_matches_the_browser() {
        // CSS: linear-gradient(in oklab, black, white) is about rgb(99,99,99) halfway (srgb gives 128)
        let ok = g("to right", "oklab", [0, 0, 0, 255], [255, 255, 255, 255]);
        let mid = color_at(&ok, 0.5).0;
        assert!((97..=101).contains(&mid[0]) && mid[0] == mid[1] && mid[1] == mid[2], "{mid:?}");
        let s = g("to right", "srgb", [0, 0, 0, 255], [255, 255, 255, 255]);
        assert_eq!(color_at(&s, 0.5).0[0], 128);
        assert_eq!(color_at(&ok, 0.0).0, [0, 0, 0, 255]);
        assert_eq!(color_at(&ok, 1.0).0, [255, 255, 255, 255]);
    }

    #[test]
    fn round_trip_keeps_colors() {
        for c in [[0x00, 0x54, 0xe3, 0xff], [0x1f, 0x6e, 0xe0, 0x80], [0xec, 0xe9, 0xd8, 0xff]] {
            assert_eq!(from_oklab(to_oklab(Rgba(c))).0, c);
        }
    }

    #[test]
    fn layers_paint_back_to_front() {
        let rect = Rect::from_min_size(Pos2::new(10.0, 10.0), Vec2::new(40.0, 20.0));
        let l = |inset: bool, dy: f32, c: u8| Layer {
            inset,
            dx: 0.0,
            dy,
            blur: 0.0,
            spread: 0.0,
            color: Rgba([c, 0, 0, 255]),
        };
        // CSS: an outline (first, on top) then a drop shadow; an inset mark on top of the background
        let shapes = box_shapes(rect, None, Color32::WHITE, &[l(false, 0.0, 1), l(false, 4.0, 2), l(true, 3.0, 3)]);
        assert_eq!(shapes.len(), 4, "two outer shadows, the fill, one inset band");
        assert!(
            matches!(&shapes[2], Shape::Rect(r) if r.fill == Color32::WHITE),
            "the background after the outer shadows"
        );
        assert!(
            matches!(&shapes[3], Shape::Rect(r) if r.fill == c32(Rgba([3, 0, 0, 255])) && r.rect.height() == 3.0),
            "the top inset band last"
        );
    }

    #[test]
    fn mesh_runs_along_the_direction() {
        let rect = Rect::from_min_size(Pos2::ZERO, Vec2::new(100.0, 20.0));
        let m = gradient_mesh(rect, &g("to right", "oklab", [0, 84, 227, 255], [31, 110, 224, 255]));
        let left = m.vertices.iter().min_by(|a, b| a.pos.x.total_cmp(&b.pos.x)).unwrap();
        let right = m.vertices.iter().max_by(|a, b| a.pos.x.total_cmp(&b.pos.x)).unwrap();
        assert_eq!((left.pos.x, right.pos.x), (0.0, 100.0));
        assert_eq!(left.color, c32(Rgba([0, 84, 227, 255])));
        assert_eq!(right.color, c32(Rgba([31, 110, 224, 255])));
        let m = gradient_mesh(rect, &g("to bottom", "srgb", [0, 0, 0, 255], [255, 255, 255, 255]));
        let top = m.vertices.iter().min_by(|a, b| a.pos.y.total_cmp(&b.pos.y)).unwrap();
        assert_eq!((top.pos.y, top.color), (0.0, Color32::BLACK));
    }
}
