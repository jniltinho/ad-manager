package main

import (
	"context"
	"errors"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"ad-manager/desktop-wails/internal/panel"
)

// Files is the service the SPA's saveFile/sendFile call in the desktop: the bytes move in Go between the panel and
// the disk, through native dialogs, never through the webview's bridge or JavaScript memory.
type Files struct {
	app   *application.App
	panel *panel.Panel

	mu     sync.Mutex
	cancel context.CancelFunc
}

// errCancelled is a dialog the operator closed without choosing: not shown as an error.
var errCancelled = errors.New("cancelled")

// chooseSave and chooseOpen are the native dialogs (the smoke build answers them itself).
var chooseSave = func(app *application.App, name string) (string, error) {
	return app.Dialog.SaveFile().SetFilename(name).CanCreateDirectories(true).PromptForSingleSelection()
}

var chooseOpen = func(app *application.App) (string, error) {
	return app.Dialog.OpenFile().CanChooseFiles(true).PromptForSingleSelection()
}

func (f *Files) saveTo(name string) (string, error) {
	path, err := chooseSave(f.app, name)
	if err == nil && path == "" {
		err = errCancelled
	}
	return path, err
}

// SaveText saves text (a CSV built in the SPA) to a file the operator picks; false when the dialog was cancelled.
func (f *Files) SaveText(name, text string) (bool, error) {
	path, err := f.saveTo(name)
	if err != nil {
		return false, ignoreCancel(err)
	}
	return true, panel.SaveText(path, text)
}

// SaveFromPanel streams the panel's apiPath ("/api/…") of session gen to a file the operator picks; false when
// cancelled.
func (f *Files) SaveFromPanel(gen uint64, apiPath, name string) (bool, error) {
	path, err := f.saveTo(name)
	if err != nil {
		return false, ignoreCancel(err)
	}
	_, _, err = f.panel.Download(context.Background(), gen, apiPath, path)
	return err == nil, err
}

// SendToPanel streams a file the operator picks as the body of POST apiPath (a backup archive) on session gen,
// emitting "adm:upload-progress" {sent, total} events; it returns the panel's JSON answer, or "" when cancelled.
func (f *Files) SendToPanel(gen uint64, apiPath string) (string, error) {
	path, err := chooseOpen(f.app)
	if err != nil || path == "" {
		return "", ignoreCancel(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.mu.Lock()
	if f.cancel != nil {
		f.cancel()
	}
	f.cancel = cancel
	f.mu.Unlock()
	defer cancel()
	body, err := f.panel.Upload(ctx, gen, apiPath, path, func(sent, total int64) {
		f.app.Event.Emit("adm:upload-progress", map[string]int64{"sent": sent, "total": total})
	})
	return string(body), err
}

// CancelUpload stops the running SendToPanel.
func (f *Files) CancelUpload() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cancel != nil {
		f.cancel()
	}
}

func ignoreCancel(err error) error {
	if errors.Is(err, errCancelled) {
		return nil
	}
	return err
}
