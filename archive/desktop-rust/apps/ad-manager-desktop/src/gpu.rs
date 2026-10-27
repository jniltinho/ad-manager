//! The wgpu side of the crash-safe graphics start (the plan and the files are `adm_core::gpu`): the backends and the
//! adapter a launch renders on.

use std::sync::Arc;

use adm_core::gpu::{Backend, Os};
use eframe::egui_wgpu::{WgpuConfiguration, WgpuSetup};
use eframe::wgpu::{self, Backends, DeviceType};

/// wgpu setup for `backend`; `None` keeps eframe's default (wgpu's own choice of backend and adapter): for `Wgpu`, on
/// macOS (Metal, or the glow renderer chosen in `main.rs`), on a launch forced by `WGPU_BACKEND`, and for `Auto` off
/// Windows.
pub fn configuration(backend: Backend, os: Os, forced: bool) -> Option<WgpuConfiguration> {
    let windows = os == Os::Windows;
    if os == Os::Macos || forced || backend == Backend::Wgpu || (backend == Backend::Auto && !windows) {
        return None;
    }
    let backends = match backend {
        Backend::Auto | Backend::Wgpu => Backends::all(),
        Backend::Gl => Backends::GL,
        Backend::Dx12 => Backends::DX12,
        Backend::Vulkan => Backends::VULKAN,
        Backend::Cpu if windows => Backends::DX12,
        Backend::Cpu => Backends::GL | Backends::VULKAN,
    };
    let mut config = WgpuConfiguration::default();
    if let WgpuSetup::CreateNew(setup) = &mut config.wgpu_setup {
        setup.instance_descriptor.backends = backends;
        let software = backend == Backend::Cpu;
        setup.native_adapter_selector = Some(Arc::new(move |adapters, surface| {
            let usable: Vec<&wgpu::Adapter> =
                adapters.iter().filter(|a| surface.is_none_or(|s| a.is_surface_supported(s))).collect();
            let infos: Vec<_> = usable.iter().map(|a| a.get_info()).collect();
            let list: Vec<_> = infos.iter().map(|i| (i.backend, i.device_type, i.name.as_str())).collect();
            pick_adapter(&list, software)
                .and_then(|i| usable.get(i))
                .map(|a| (*a).clone())
                .ok_or_else(|| "no usable wgpu adapter".to_owned())
        }));
    }
    Some(config)
}

/// Index of the adapter to render on. Normal: a hardware OpenGL one first (DX12 presentation flickers on Intel
/// graphics), then discrete, integrated, virtual, other, CPU. Software: a CPU adapter, the output-owning WARP entry
/// first (GPU-less VMs list WARP twice and the other one gives a black window), else the first usable one so the
/// window still opens (NVIDIA's EGL hides llvmpipe) — which may then be hardware.
pub fn pick_adapter(adapters: &[(wgpu::Backend, DeviceType, &str)], software: bool) -> Option<usize> {
    let warp = |name: &str| name.contains("Microsoft Basic Render Driver");
    if software && !adapters.iter().any(|&(_, kind, name)| kind == DeviceType::Cpu || warp(name)) {
        return if adapters.is_empty() { None } else { Some(0) };
    }
    let rank = |&(backend, kind, name): &(wgpu::Backend, DeviceType, &str)| {
        let device = match kind {
            DeviceType::DiscreteGpu => 0,
            DeviceType::IntegratedGpu => 1,
            DeviceType::VirtualGpu => 2,
            DeviceType::Other => 3,
            DeviceType::Cpu => 4,
        };
        if software {
            let cpu = kind == DeviceType::Cpu || warp(name);
            // WARP is listed twice on GPU-less machines; the non-CPU entry owns the output.
            return (!cpu, !(warp(name) && kind != DeviceType::Cpu), device);
        }
        (false, !(backend == wgpu::Backend::Gl && kind != DeviceType::Cpu), device)
    };
    (0..adapters.len()).min_by_key(|&i| rank(&adapters[i]))
}

#[cfg(test)]
mod tests {
    use super::*;
    use wgpu::Backend as W;

    #[test]
    fn adapter_choice() {
        let intel = [(W::Dx12, DeviceType::IntegratedGpu, "Intel"), (W::Gl, DeviceType::IntegratedGpu, "Intel")];
        assert_eq!(pick_adapter(&intel, false), Some(1), "hardware OpenGL first");
        let vm = [
            (W::Dx12, DeviceType::Cpu, "Microsoft Basic Render Driver"),
            (W::Dx12, DeviceType::Other, "Microsoft Basic Render Driver"),
            (W::Gl, DeviceType::Cpu, "llvmpipe"),
        ];
        assert_eq!(pick_adapter(&vm, false), Some(1));
        assert_eq!(pick_adapter(&vm, true), Some(1), "output-owning WARP");
        let linux = [(W::Vulkan, DeviceType::DiscreteGpu, "RTX"), (W::Gl, DeviceType::Cpu, "llvmpipe")];
        assert_eq!(pick_adapter(&linux, true), Some(1));
        let nvidia_egl = [(W::Vulkan, DeviceType::DiscreteGpu, "RTX")];
        assert_eq!(pick_adapter(&nvidia_egl, true), Some(0), "no software adapter: still opens");
        let two = [(W::Vulkan, DeviceType::IntegratedGpu, "Intel"), (W::Vulkan, DeviceType::DiscreteGpu, "RTX")];
        assert_eq!(pick_adapter(&two, true), Some(0), "no software adapter: the first usable one");
        assert_eq!(pick_adapter(&[], false), None);
    }

    #[test]
    fn eframe_defaults_where_the_plan_does_not_choose() {
        assert!(configuration(Backend::Auto, Os::Linux, false).is_none(), "Linux auto: wgpu's own choice");
        assert!(configuration(Backend::Gl, Os::Macos, false).is_none(), "macOS: Metal or glow, never reconfigured");
        assert!(configuration(Backend::Dx12, Os::Windows, true).is_none(), "WGPU_BACKEND forced");
        assert!(configuration(Backend::Auto, Os::Windows, false).is_some(), "Windows auto: hardware GL first");
        assert!(configuration(Backend::Wgpu, Os::Windows, false).is_none(), "wgpu: its own choice");
        assert!(configuration(Backend::Cpu, Os::Linux, false).is_some());
    }
}
