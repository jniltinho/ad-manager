//go:build linux && !gtk3

package main

/*
#cgo pkg-config: gtk4
#include <gtk/gtk.h>

// adm_set_icon makes name the default and the window's icon: GTK4 has no icon from bytes, only icon theme names, so
// dir (with hicolor/256x256/apps/<name>.png, merged with the system's hicolor theme) joins the theme's search path.
static void adm_set_icon(void *window, const char *dir, const char *name) {
	gtk_icon_theme_add_search_path(gtk_icon_theme_get_for_display(gdk_display_get_default()), dir);
	gtk_window_set_default_icon_name(name);
	gtk_window_set_icon_name(GTK_WINDOW(window), name);
}
*/
import "C"

import (
	"bytes"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// setWindowIcon puts the app's icon on the window (instead of the window system's default), from a copy of the
// embedded PNG in the user's cache folder.
func setWindowIcon(w *application.WebviewWindow) {
	base, err := os.UserCacheDir()
	if err != nil {
		return
	}
	dir := filepath.Join(base, "ad-manager", "icons")
	apps := filepath.Join(dir, "hicolor", "256x256", "apps")
	file := filepath.Join(apps, iconName+".png")
	if old, err := os.ReadFile(file); err != nil || !bytes.Equal(old, appIcon) {
		if os.MkdirAll(apps, 0o700) != nil || os.WriteFile(file, appIcon, 0o600) != nil {
			return
		}
	}
	cdir, cname := C.CString(dir), C.CString(iconName)
	defer C.free(unsafe.Pointer(cdir))
	defer C.free(unsafe.Pointer(cname))
	application.InvokeSync(func() { C.adm_set_icon(w.NativeWindow(), cdir, cname) })
}
