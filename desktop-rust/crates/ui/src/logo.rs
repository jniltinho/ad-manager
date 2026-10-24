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

    pub fn image(&self) -> Image<'static> {
        Image::from_bytes(self.uri.clone(), egui::load::Bytes::Shared(self.bytes.clone())).texture_options(SMOOTH)
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

/// A logo drawn several times smaller than its pixels (1320 x 440 shown 40 high): mipmaps, so the edges stay smooth
/// instead of the jagged look of a plain linear minification.
const SMOOTH: egui::TextureOptions = egui::TextureOptions {
    magnification: egui::TextureFilter::Linear,
    minification: egui::TextureFilter::Linear,
    wrap_mode: egui::TextureWrapMode::ClampToEdge,
    mipmap_mode: Some(egui::TextureFilter::Linear),
};

pub fn product_navy() -> Image<'static> {
    Image::from_bytes("bytes://ad-manager-logo.png", assets::LOGO_NAVY).texture_options(SMOOTH)
}

pub fn product_white() -> Image<'static> {
    Image::from_bytes("bytes://ad-manager-logo-light.png", assets::LOGO_WHITE).texture_options(SMOOTH)
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
}
