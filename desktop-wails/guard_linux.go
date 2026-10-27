//go:build linux && !gtk3

package main

/*
#cgo pkg-config: gtk4 webkitgtk-6.0
#include <gtk/gtk.h>
#include <webkit/webkit.h>

extern void admNavigationBlocked(char *uri);

// adm_decide keeps the webview on the app: a navigation to anything but the app's own wails:// origin, and every
// new-window request, is ignored.
static gboolean adm_decide(WebKitWebView *wv, WebKitPolicyDecision *d, WebKitPolicyDecisionType type, gpointer data) {
	if (type == WEBKIT_POLICY_DECISION_TYPE_NEW_WINDOW_ACTION) {
		webkit_policy_decision_ignore(d);
		admNavigationBlocked("new window");
		return TRUE;
	}
	if (type != WEBKIT_POLICY_DECISION_TYPE_NAVIGATION_ACTION) return FALSE;
	WebKitNavigationAction *a = webkit_navigation_policy_decision_get_navigation_action(WEBKIT_NAVIGATION_POLICY_DECISION(d));
	const char *uri = webkit_uri_request_get_uri(webkit_navigation_action_get_request(a));
	if (uri != NULL && g_str_has_prefix(uri, "wails://")) return FALSE;
	webkit_policy_decision_ignore(d);
	admNavigationBlocked((char *)(uri ? uri : ""));
	return TRUE;
}

static GtkWidget *adm_find_webview(GtkWidget *w) {
	if (WEBKIT_IS_WEB_VIEW(w)) return w;
	for (GtkWidget *c = gtk_widget_get_first_child(w); c != NULL; c = gtk_widget_get_next_sibling(c)) {
		GtkWidget *f = adm_find_webview(c);
		if (f != NULL) return f;
	}
	return NULL;
}

static int adm_guard(void *window) {
	GtkWidget *wv = adm_find_webview(GTK_WIDGET(window));
	if (wv == NULL) return 0;
	g_signal_connect(wv, "decide-policy", G_CALLBACK(adm_decide), NULL);
	return 1;
}
*/
import "C"

import (
	"errors"
	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//export admNavigationBlocked
func admNavigationBlocked(uri *C.char) {
	slog.Warn("navigation away from the app blocked", "to", C.GoString(uri))
}

// guardNavigation connects the webview's decide-policy signal (GTK main thread) so the window never leaves the app.
func guardNavigation(w *application.WebviewWindow) error {
	ok := false
	application.InvokeSync(func() { ok = C.adm_guard(w.NativeWindow()) == 1 })
	if !ok {
		return errors.New("navigation guard: the webview was not found in the window")
	}
	return nil
}
