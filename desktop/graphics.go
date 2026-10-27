package main

import (
	"fmt"
	"os"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// safeGraphics starts the webview with GPU acceleration off (--safe-gpu or [graphics] safe): a workaround preset
// for a driver that cannot render it, not a guarantee that no hardware path is used.
func safeGraphics(o *application.Options, w *application.WebviewWindowOptions) {
	switch runtime.GOOS {
	case "linux":
		// GTK's software renderer, and WebKit without DMA-BUF and compositing
		for k, v := range map[string]string{"GSK_RENDERER": "cairo", "WEBKIT_DISABLE_DMABUF_RENDERER": "1",
			"WEBKIT_DISABLE_COMPOSITING_MODE": "1", "LIBGL_ALWAYS_SOFTWARE": "1"} {
			_ = os.Setenv(k, v)
		}
		w.Linux.WebviewGpuPolicy = application.WebviewGpuPolicyNever
	case "windows":
		o.Windows.AdditionalBrowserArgs = append(o.Windows.AdditionalBrowserArgs, "--disable-gpu")
	default:
		fmt.Fprintln(os.Stderr, "--safe-gpu does not apply on", runtime.GOOS)
	}
}
