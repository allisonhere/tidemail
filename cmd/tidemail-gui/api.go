//go:build desktop

package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	appcore "github.com/allisonhere/tide/internal/app"
	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

type DesktopAPI struct {
	mu      sync.RWMutex
	ctx     context.Context
	service *appcore.Service
}

type PickedAttachment struct {
	Name string `json:"name"`
	Data []byte `json:"data"`
	Size int64  `json:"size"`
}

func (a *DesktopAPI) startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
}

func (a *DesktopAPI) shutdown(context.Context) {
	a.mu.Lock()
	a.ctx = nil
	a.mu.Unlock()
}

func (a *DesktopAPI) context() context.Context {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

func (a *DesktopAPI) emit(name string, data any) {
	runtime.EventsEmit(a.context(), name, data)
}

func (a *DesktopAPI) Bootstrap() (appcore.Bootstrap, error) { return a.service.Bootstrap() }

func (a *DesktopAPI) ListMessages(mailboxID int64, unified, unreadOnly bool) (appcore.MessagePage, error) {
	return a.service.ListMessages(mailboxID, unified, unreadOnly)
}

func (a *DesktopAPI) Search(query string) (appcore.MessagePage, error) {
	return a.service.Search(query)
}

func (a *DesktopAPI) SyncMailbox(mailboxID int64) (appcore.SyncResult, error) {
	return a.service.SyncMailbox(a.context(), mailboxID)
}

func (a *DesktopAPI) Message(id int64) (appcore.MessageDetail, error) {
	return a.service.Message(id)
}

func (a *DesktopAPI) SetRead(id int64, read bool) error {
	return a.service.SetRead(a.context(), id, read)
}

func (a *DesktopAPI) SetStarred(id int64, starred bool) error {
	return a.service.SetStarred(a.context(), id, starred)
}

func (a *DesktopAPI) Move(id, targetMailboxID int64) error {
	return a.service.Move(a.context(), id, targetMailboxID)
}

func (a *DesktopAPI) Archive(id int64) error { return a.service.Archive(a.context(), id) }
func (a *DesktopAPI) Delete(id int64) error  { return a.service.Delete(a.context(), id) }

func (a *DesktopAPI) PickAttachments() ([]PickedAttachment, error) {
	paths, err := runtime.OpenMultipleFilesDialog(a.context(), runtime.OpenDialogOptions{Title: "Attach files"})
	if err != nil {
		return nil, err
	}
	picked := make([]PickedAttachment, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		picked = append(picked, PickedAttachment{Name: filepath.Base(path), Data: data, Size: int64(len(data))})
	}
	return picked, nil
}

func (a *DesktopAPI) SaveAttachment(messageID, attachmentID int64) (string, error) {
	attachment, err := a.service.AttachmentData(messageID, attachmentID)
	if err != nil {
		return "", err
	}
	target, err := runtime.SaveFileDialog(a.context(), runtime.SaveDialogOptions{Title: "Save attachment", DefaultFilename: attachment.Filename})
	if err != nil || target == "" {
		return "", err
	}
	if err := os.WriteFile(target, attachment.Data, 0o600); err != nil {
		return "", err
	}
	return target, nil
}

func (a *DesktopAPI) Send(request appcore.ComposeRequest) error {
	return a.service.Send(a.context(), request)
}

func (a *DesktopAPI) SaveDraft(draft db.Draft) (int64, error) {
	return a.service.SaveDraft(draft)
}

func (a *DesktopAPI) Draft(id int64) (db.Draft, error) { return a.service.Draft(id) }
func (a *DesktopAPI) DeleteDraft(id int64) error       { return a.service.DeleteDraft(id) }
func (a *DesktopAPI) Contacts() ([]db.Contact, error)  { return a.service.Contacts() }
func (a *DesktopAPI) Rules() ([]db.RuleRecord, error)  { return a.service.Rules() }

func (a *DesktopAPI) Summarize(messageID int64) (string, error) {
	return a.service.Summarize(a.context(), messageID)
}

func (a *DesktopAPI) CheckGrammar(body string) (string, error) {
	return a.service.CheckGrammar(a.context(), body)
}

type DesktopSettings struct {
	Accounts []config.AccountConfig `json:"accounts"`
}

func (a *DesktopAPI) Settings() DesktopSettings {
	return DesktopSettings{Accounts: a.service.Config().Accounts}
}

func (a *DesktopAPI) SaveSettings(settings DesktopSettings) error {
	cfg := a.service.Config()
	cfg.Accounts = settings.Accounts
	if err := a.service.SaveConfig(cfg); err != nil {
		return err
	}
	if _, err := a.service.Bootstrap(); err != nil {
		return err
	}
	a.emit("config.changed", nil)
	return nil
}

func (a *DesktopAPI) SaveDesktopLayout(layout string, folderWidth, messageWidth int) error {
	cfg := a.service.Config()
	cfg.Display.DesktopLayout = config.NormalizeDesktopLayout(layout)
	if folderWidth >= 180 && folderWidth <= 360 {
		cfg.Display.DesktopFolderWidth = folderWidth
	}
	if messageWidth >= 300 && messageWidth <= 560 {
		cfg.Display.DesktopMessageWidth = messageWidth
	}
	return a.service.SaveConfig(cfg)
}
