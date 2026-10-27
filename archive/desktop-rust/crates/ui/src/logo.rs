//! Logos: the AD Manager product logo (navy or white ink) and the company logo the panel serves, accepted only when
//! it decodes and draws something (a broken image falls back to the product logo, never to nothing).

use std::sync::Arc;

use adm_core::assets;
use egui::Image;

/// A company logo that decoded, with its egui image URI.
#[derive(Clone)]
pub struct Logo {
    uri: String,
    bytes: Arc<[u8]>,
}

impl Logo {
    /// The logo when `bytes` is a PNG, JPEG or SVG that decodes and draws (at most 2 MiB, checked by the client).
    pub fn decode(bytes: Vec<u8>, content_type: &str) -> Option<Self> {
        let ext = match content_type {
            "image/png" | "image/jpeg" => {
                image::load_from_memory(&bytes).ok()?;
                if content_type == "image/png" { "png" } else { "jpg" }
            }
            "image/svg+xml" => {
                drawable_svg(&bytes).then_some(())?;
                "svg"
            }
            _ => return None,
        };
        // a new URI per logo: egui caches images by URI
        let n = std::time::SystemTime::now().duration_since(std::time::UNIX_EPOCH).map_or(0, |d| d.as_nanos());
        Some(Self { uri: format!("bytes://company-logo-{n}.{ext}"), bytes: bytes.into() })
    }

    /// The logo fitted into `fit` points (an SVG is rasterized at its drawn size by egui's loader).
    pub fn image(&self, ui: &egui::Ui, fit: egui::Vec2) -> Image<'static> {
        if !self.uri.ends_with(".svg")
            && let Some(img) = sized(ui, &self.uri, &self.bytes, fit)
        {
            return img;
        }
        Image::from_bytes(self.uri.clone(), egui::load::Bytes::Shared(self.bytes.clone()))
    }
}

/// An SVG that parses, has a size and draws at least one pixel.
pub fn drawable_svg(b: &[u8]) -> bool {
    let Ok(tree) = resvg::usvg::Tree::from_data(b, &resvg::usvg::Options::default()) else { return false };
    let size = tree.size();
    if size.width() <= 0.0 || size.height() <= 0.0 {
        return false;
    }
    let Some(mut px) = resvg::tiny_skia::Pixmap::new(64, 64) else { return false };
    let t = resvg::tiny_skia::Transform::from_scale(64.0 / size.width(), 64.0 / size.height());
    resvg::render(&tree, t, &mut px.as_mut());
    px.pixels().iter().any(|p| p.alpha() > 0)
}

/// The logo textures made by `sized`, oldest first.
type Cache = Vec<(egui::Id, egui::TextureHandle, egui::Vec2)>;

/// A raster logo fitted into `fit` points, its pixels shrunk on the CPU to the exact device pixels it is drawn at:
/// the product logo is 1320 x 440 drawn 40 high, and the GPU's linear minification (wgpu has no mipmaps) makes it
/// jagged. Premultiplied before the resize, so transparent edges get no dark fringe; cached per size.
fn sized(ui: &egui::Ui, key: &str, bytes: &[u8], fit: egui::Vec2) -> Option<Image<'static>> {
    let ctx = ui.ctx();
    let ppp = ctx.pixels_per_point();
    let id = egui::Id::new((key, fit.x.to_bits(), fit.y.to_bits(), ppp.to_bits()));
    let hit = ctx.data_mut(|d| {
        d.get_temp_mut_or_default::<Cache>(egui::Id::new("adm-logo-cache")).iter().find(|e| e.0 == id).cloned()
    });
    if let Some((_, tex, size)) = hit {
        return Some(Image::new((tex.id(), size)));
    }
    let mut img = image::load_from_memory(bytes).ok()?.to_rgba8();
    let (w, h) = (img.width() as f32, img.height() as f32);
    let k = (fit.x / w).min(fit.y / h);
    if !(k.is_finite() && k > 0.0) {
        return None;
    }
    let size = egui::vec2(w * k, h * k);
    for p in img.pixels_mut() {
        let a = u16::from(p.0[3]);
        for c in &mut p.0[..3] {
            *c = ((u16::from(*c) * a + 127) / 255) as u8;
        }
    }
    let (pw, ph) = (((size.x * ppp).round() as u32).max(1), ((size.y * ppp).round() as u32).max(1));
    let small = image::imageops::resize(&img, pw, ph, image::imageops::FilterType::Triangle);
    let color = egui::ColorImage::from_rgba_premultiplied([pw as usize, ph as usize], small.as_raw());
    let tex = ctx.load_texture(key, color, egui::TextureOptions::LINEAR);
    ctx.data_mut(|d| {
        // the newest few kept: a replaced company logo or an old scale drops its texture (the GPU memory with it)
        let cache = d.get_temp_mut_or_default::<Cache>(egui::Id::new("adm-logo-cache"));
        cache.push((id, tex.clone(), size));
        if cache.len() > 8 {
            drop(cache.remove(0));
        }
    });
    Some(Image::new((tex.id(), size)))
}

pub fn product_navy(ui: &egui::Ui, fit: egui::Vec2) -> Image<'static> {
    sized(ui, "ad-manager-logo.png", assets::LOGO_NAVY, fit)
        .unwrap_or_else(|| Image::from_bytes("bytes://ad-manager-logo.png", assets::LOGO_NAVY))
}

pub fn product_white(ui: &egui::Ui, fit: egui::Vec2) -> Image<'static> {
    sized(ui, "ad-manager-logo-light.png", assets::LOGO_WHITE, fit)
        .unwrap_or_else(|| Image::from_bytes("bytes://ad-manager-logo-light.png", assets::LOGO_WHITE))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn logos() {
        assert!(Logo::decode(assets::LOGO_NAVY.to_vec(), "image/png").is_some());
        assert!(Logo::decode(b"not a png".to_vec(), "image/png").is_none());
        assert!(Logo::decode(assets::LOGO_NAVY.to_vec(), "text/html").is_none());
        let ok = br#"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10" fill="red"/></svg>"#;
        assert!(drawable_svg(ok));
        let empty = br#"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><text>x</text></svg>"#;
        assert!(!drawable_svg(empty));
        assert!(!drawable_svg(b"<svg"));
    }

    #[test]
    fn sized_logo_is_the_drawn_pixels() {
        let ctx = egui::Context::default();
        ctx.set_pixels_per_point(2.0);
        let mut out = ctx.run_ui(egui::RawInput::default(), |ui| {
            let img = sized(ui, "t", assets::LOGO_NAVY, egui::vec2(256.0, 40.0)).expect("decodes");
            let size = img.size().expect("a sized texture");
            assert!((size.y - 40.0).abs() < 0.01 && size.x <= 256.0, "{size:?}");
            assert!(sized(ui, "t", b"not an image", egui::vec2(10.0, 10.0)).is_none());
        });
        out.textures_delta.clear(); // no renderer to hand the uploaded texture to
    }
}
