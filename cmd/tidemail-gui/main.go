//go:build desktop

package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	appcore "github.com/allisonhere/tide/internal/app"
	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/profilelock"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	lockPath, err := config.ProfileLockPath()
	if err != nil {
		fatal(err)
	}
	profile, err := profilelock.Acquire(lockPath)
	if err != nil {
		fatal(err)
	}
	defer profile.Close() //nolint:errcheck

	cfg, err := config.Load()
	if err != nil {
		fatal(fmt.Errorf("load config: %w", err))
	}
	database, err := db.Open()
	if err != nil {
		fatal(fmt.Errorf("open database: %w", err))
	}
	defer database.Close()

	api := &DesktopAPI{}
	service := appcore.New(database, cfg, func(event appcore.Event) {
		api.emit(event.Name, event.Data)
	})
	defer service.Close()
	api.service = service

	err = wails.Run(&options.App{
		Title:            "TideMail",
		Width:            1440,
		Height:           900,
		MinWidth:         900,
		MinHeight:        620,
		AssetServer:      &assetserver.Options{Assets: assets},
		BackgroundColour: &options.RGBA{R: 13, G: 17, B: 23, A: 1},
		OnStartup:        api.startup,
		OnShutdown:       api.shutdown,
		Bind:             []any{api},
		Menu:             desktopMenu(api),
	})
	if err != nil {
		fatal(err)
	}
}

func desktopMenu(api *DesktopAPI) *menu.Menu {
	root := menu.NewMenu()
	file := root.AddSubmenu("File")
	file.AddText("New Message", keys.CmdOrCtrl("n"), func(*menu.CallbackData) { api.emit("desktop.command", "compose") })
	file.AddText("Sync Mail", keys.CmdOrCtrl("r"), func(*menu.CallbackData) { api.emit("desktop.command", "sync") })
	file.AddSeparator()
	file.AddText("Preferences", keys.CmdOrCtrl(","), func(*menu.CallbackData) { api.emit("desktop.command", "settings") })

	view := root.AddSubmenu("View")
	view.AddText("Native Layout", nil, func(*menu.CallbackData) { api.emit("desktop.command", "layout.native") })
	view.AddText("Modern Layout", nil, func(*menu.CallbackData) { api.emit("desktop.command", "layout.modern") })
	view.AddText("Focus Search", keys.CmdOrCtrl("f"), func(*menu.CallbackData) { api.emit("desktop.command", "search") })

	message := root.AddSubmenu("Message")
	message.AddText("Reply", keys.Key("r"), func(*menu.CallbackData) { api.emit("desktop.command", "reply") })
	message.AddText("Archive", keys.Key("a"), func(*menu.CallbackData) { api.emit("desktop.command", "archive") })
	message.AddText("Star or Unstar", keys.Key("s"), func(*menu.CallbackData) { api.emit("desktop.command", "star") })
	message.AddText("Read or Unread", keys.Key("u"), func(*menu.CallbackData) { api.emit("desktop.command", "read") })
	message.AddText("Delete", keys.Key("delete"), func(*menu.CallbackData) { api.emit("desktop.command", "delete") })
	return root
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "tidemail-gui:", err)
	os.Exit(1)
}
