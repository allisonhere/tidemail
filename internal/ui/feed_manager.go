package ui

import (
	"context"
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/feed"
	"github.com/allisonhere/tide/internal/greader"
	"github.com/allisonhere/tide/internal/opml"
)

type fmMode int

const (
	// Modes are mutually exclusive screens; pane focus only matters inside fmList/fmEdit split layouts. -allie
	fmList fmMode = iota
	fmEdit
	fmFolderEdit
	fmImport
	fmConfirmDelete
	fmMove
)

type fmPaneFocus int

const (
	fmPaneList fmPaneFocus = iota
	fmPaneDetail
)

type fmRowKind int

const (
	fmRowFolder fmRowKind = iota
	fmRowFeed
)

type fmRow struct {
	// Rows are a render/navigation projection, not storage; folder/feed ordering is rebuilt from current data. -allie
	kind     fmRowKind
	folderID int64
	feedID   int64
}

const (
	fmAddSourceLocal = iota
	fmAddSourceGReader
)

const fmFieldBack = -1

const (
	fmFieldAddSource = 5 + iota
	fmFieldGReaderURL
	fmFieldGReaderLogin
	fmFieldGReaderPassword
	fmFieldImportPath
	fmFieldMoveFolder
)

var fmAddSourceLabels = []string{"Local", "GReader"}

type feedManagerSource interface {
	// Sources let the same manager browse local DB feeds or read-only remote subscription snapshots. -allie
	Load() ([]db.Feed, []db.Folder, error)
	Name() string
	Editable() bool
}

type dbFeedManagerSource struct {
	database *db.DB
}

func (s dbFeedManagerSource) Load() ([]db.Feed, []db.Folder, error) {
	if s.database == nil {
		return nil, nil, nil
	}
	feeds, err := s.database.ListFeeds()
	if err != nil {
		return nil, nil, err
	}
	folders, err := s.database.ListFolders()
	if err != nil {
		return nil, nil, err
	}
	return feeds, folders, nil
}

func (s dbFeedManagerSource) Name() string { return "Local" }

func (s dbFeedManagerSource) Editable() bool { return true }

type staticFeedManagerSource struct {
	name     string
	feeds    []db.Feed
	folders  []db.Folder
	editable bool
}

func (s staticFeedManagerSource) Load() ([]db.Feed, []db.Folder, error) {
	feeds := append([]db.Feed(nil), s.feeds...)
	folders := append([]db.Folder(nil), s.folders...)
	return feeds, folders, nil
}

func (s staticFeedManagerSource) Name() string { return s.name }

func (s staticFeedManagerSource) Editable() bool { return s.editable }

type FeedManager struct {
	// Persistent data and row projection are separated so edits can reload without losing navigation state. -allie
	db               *db.DB
	source           feedManagerSource
	feeds            []db.Feed
	folders          []db.Folder
	collapsedFolders map[int64]bool
	rows             []fmRow
	cursor           int
	mode             fmMode
	editTarget       int64 // 0 = new feed
	folderEditTarget int64
	moveTarget       int64 // feed being moved in fmMove mode
	moveCursor       int   // index into folderOptionsForMove()

	titleInput           textinput.Model
	urlInput             textinput.Model
	importInput          textinput.Model
	newFolderInput       textinput.Model
	greaderURLInput      textinput.Model
	greaderLoginInput    textinput.Model
	greaderPasswordInput textinput.Model
	// focusedField uses sentinel values for picker/back rows so keyboard movement stays linear across mixed controls. -allie
	focusedField       int // 0=title, 1=url, 2=folder, 3=new folder, 4=color
	addSourceIdx       int
	folderCursor       int
	showNewFolder      bool
	colorCursor        int
	remoteSettingsEdit bool

	shouldExit   bool
	browseFeedID int64
	statusMsg    string
	busy         bool
	busyMsg      string
	paneFocus    fmPaneFocus
}

func (fm *FeedManager) setData(feeds []db.Feed, folders []db.Folder) {
	fm.feeds = append([]db.Feed(nil), feeds...)
	fm.folders = append([]db.Folder(nil), folders...)
	fm.rebuildRows()
	fm.cursor = clamp(fm.cursor, 0, max(0, len(fm.rows)-1))
	fm.folderCursor = clamp(fm.folderCursor, 0, max(0, len(fm.folderOptions())-1))
	fm.colorCursor = clamp(fm.colorCursor, 0, max(0, len(folderColorOptions)-1))
}

func NewFeedManager(database *db.DB) FeedManager {
	return NewFeedManagerWithSource(database, config.SourceConfig{})
}

func NewFeedManagerWithSource(database *db.DB, sourceCfg config.SourceConfig) FeedManager {
	return newFeedManager(database, dbFeedManagerSource{database: database}, sourceCfg)
}

func NewRemoteFeedManager(sourceName string, feeds []db.Feed, folders []db.Folder) FeedManager {
	return newFeedManager(nil, staticFeedManagerSource{
		name:     sourceName,
		feeds:    feeds,
		folders:  folders,
		editable: false,
	}, config.SourceConfig{})
}

func newFeedManager(database *db.DB, source feedManagerSource, sourceCfg config.SourceConfig) FeedManager {
	title := textinput.New()
	title.Placeholder = "Feed title"
	title.CharLimit = 200

	urlInput := textinput.New()
	urlInput.Placeholder = "https://example.com/feed.xml"
	urlInput.CharLimit = 500

	imp := textinput.New()
	imp.Placeholder = "path to .opml file"
	imp.CharLimit = 500

	newFolder := textinput.New()
	newFolder.Placeholder = "Folder name"
	newFolder.CharLimit = 120

	greaderURL := textinput.New()
	greaderURL.Placeholder = "https://rss.example.com/api/greader.php"
	greaderURL.CharLimit = 500
	greaderURL.SetValue(sourceCfg.GReaderURL)

	greaderLogin := textinput.New()
	greaderLogin.Placeholder = "alice"
	greaderLogin.CharLimit = 200
	greaderLogin.SetValue(sourceCfg.GReaderLogin)

	greaderPassword := textinput.New()
	greaderPassword.Placeholder = "API password"
	greaderPassword.CharLimit = 500
	greaderPassword.EchoMode = textinput.EchoPassword
	greaderPassword.EchoCharacter = '●'
	greaderPassword.SetValue(sourceCfg.GReaderPassword)

	fm := FeedManager{
		db:                   database,
		source:               source,
		collapsedFolders:     map[int64]bool{},
		titleInput:           title,
		urlInput:             urlInput,
		importInput:          imp,
		newFolderInput:       newFolder,
		greaderURLInput:      greaderURL,
		greaderLoginInput:    greaderLogin,
		greaderPasswordInput: greaderPassword,
	}
	fm.reload()
	return fm
}

func (fm *FeedManager) reload() {
	switch {
	case fm.source != nil:
		if feeds, folders, err := fm.source.Load(); err == nil {
			fm.feeds = feeds
			fm.folders = folders
		}
	case fm.db != nil:
		feeds, _ := fm.db.ListFeeds()
		folders, _ := fm.db.ListFolders()
		fm.feeds = feeds
		fm.folders = folders
	}
	fm.rebuildRows()
	fm.cursor = clamp(fm.cursor, 0, max(0, len(fm.rows)-1))
	fm.folderCursor = clamp(fm.folderCursor, 0, max(0, len(fm.folderOptions())-1))
	fm.colorCursor = clamp(fm.colorCursor, 0, max(0, len(folderColorOptions)-1))
}

func (fm *FeedManager) rebuildRows() {
	fm.rows = buildFeedManagerRows(fm.feeds, fm.folders)
}

func (fm FeedManager) managerRows() []fmRow {
	if len(fm.rows) > 0 {
		return fm.rows
	}
	return buildFeedManagerRows(fm.feeds, fm.folders)
}

func buildFeedManagerRows(feeds []db.Feed, folders []db.Folder) []fmRow {
	// Folder rows emit their child feeds immediately so keyboard order matches the visual tree. -allie
	rows := make([]fmRow, 0, len(feeds)+len(folders))
	folderIDs := make(map[int64]bool, len(folders))
	for _, folder := range folders {
		folderIDs[folder.ID] = true
	}
	emitted := make(map[int64]bool, len(feeds))
	for _, folder := range folders {
		rows = append(rows, fmRow{kind: fmRowFolder, folderID: folder.ID})
		for _, feed := range feeds {
			if feed.FolderID == folder.ID {
				rows = append(rows, fmRow{kind: fmRowFeed, feedID: feed.ID})
				emitted[feed.ID] = true
			}
		}
	}
	// Orphans (FolderID == 0 or dangling FolderID) fall to the bottom of the list.
	for _, feed := range feeds {
		if emitted[feed.ID] {
			continue
		}
		if feed.FolderID != 0 && folderIDs[feed.FolderID] {
			continue
		}
		rows = append(rows, fmRow{kind: fmRowFeed, feedID: feed.ID})
	}
	return rows
}

func (fm FeedManager) sourceName() string {
	if fm.source != nil && strings.TrimSpace(fm.source.Name()) != "" {
		return strings.TrimSpace(fm.source.Name())
	}
	return "Local"
}

func (fm FeedManager) editable() bool {
	if fm.source != nil {
		return fm.source.Editable()
	}
	return true
}

func (fm FeedManager) listSectionTitle() string {
	if fm.editable() {
		return "FOLDERS + FEEDS"
	}
	return "SUBSCRIPTIONS"
}

func (fm FeedManager) emptyStateMessage() string {
	if fm.editable() {
		return "NO FEEDS OR FOLDERS CONFIGURED"
	}
	return "NO SUBSCRIPTIONS CONFIGURED"
}

func (fm FeedManager) listModeTitle() string {
	if fm.editable() {
		return "DETAILS"
	}
	return "SUBSCRIPTION"
}

func (fm FeedManager) listPaneFocused() bool {
	return fm.mode == fmList || fm.paneFocus == fmPaneList
}

// feedManagerRightPaneWidth returns the workspace column width used by viewSplit.
// It must stay in sync with View → viewSplit so text inputs can set bubbles Width
// for horizontal scrolling (renderTextInput previously only set Width on a copy).
func feedManagerRightPaneWidth(windowWidth int) int {
	if windowWidth <= 0 {
		return 18
	}
	winW := min(windowWidth-4, 74)
	contentW := min(winW, 74)
	leftW := clamp(contentW/4, 18, 20)
	if contentW-leftW-1 < 24 {
		leftW = max(18, contentW-25)
	}
	return max(18, contentW-leftW-1)
}

// editInputCharWidth matches renderTextInput's inner width for a detail column width.
func editInputCharWidth(rightPaneWidth int) int {
	fieldW := max(1, rightPaneWidth-2)
	contentW := max(1, fieldW-3)
	return max(1, contentW-2)
}

func (fm *FeedManager) syncTextInputWidthsForRightPane(rightPaneWidth int) {
	w := editInputCharWidth(rightPaneWidth)
	fm.titleInput.Width = w
	fm.urlInput.Width = w
	fm.newFolderInput.Width = w
	fm.importInput.Width = w
	fm.greaderURLInput.Width = w
	fm.greaderLoginInput.Width = w
	fm.greaderPasswordInput.Width = w
}

func (fm *FeedManager) setBrowseOnlyStatus() {
	fm.statusMsg = strings.ToUpper(fm.sourceName()) + " IS BROWSE-ONLY"
}

func (fm *FeedManager) focusAdd() {
	// Add starts on the back/section control so users can choose Local vs GReader before entering fields. -allie
	fm.mode = fmEdit
	fm.paneFocus = fmPaneDetail
	fm.editTarget = 0
	fm.folderEditTarget = 0
	fm.remoteSettingsEdit = false
	fm.addSourceIdx = fmAddSourceLocal
	fm.titleInput.Reset()
	fm.urlInput.Reset()
	fm.newFolderInput.Reset()
	fm.focusedField = fmFieldBack
	fm.folderCursor = 0
	fm.showNewFolder = false
	fm.colorCursor = 0
	fm.statusMsg = ""
	fm.busy = false
	fm.busyMsg = ""
	fm.focusCurrentEditField()
}

func (fm *FeedManager) prefillAddFormFromSelectedRemoteFeed() {
	if fm.mode != fmEdit || fm.editTarget != 0 {
		return
	}
	feed := fm.selectedFeedRow()
	if !fm.feedIsRemote(feed) {
		return
	}

	fm.addSourceIdx = fmAddSourceGReader
	fm.titleInput.Reset()
	fm.urlInput.Reset()
	fm.urlInput.SetValue(feed.URL)
	fm.newFolderInput.Reset()
	fm.folderCursor = 0
	fm.showNewFolder = false
	fm.colorCursor = 0
	fm.focusedField = fmFieldBack
}

func (fm *FeedManager) focusRemoteSettingsEdit(feed db.Feed) {
	fm.mode = fmEdit
	fm.paneFocus = fmPaneDetail
	fm.editTarget = feed.ID
	fm.folderEditTarget = 0
	fm.remoteSettingsEdit = true
	fm.addSourceIdx = fmAddSourceGReader
	fm.titleInput.Reset()
	fm.titleInput.SetValue(feed.Title)
	fm.urlInput.Reset()
	fm.urlInput.SetValue(feed.URL)
	fm.newFolderInput.Reset()
	fm.setFolderCursorForID(feed.FolderID)
	fm.focusedField = fmFieldBack
	fm.statusMsg = ""
	fm.busy = false
	fm.busyMsg = ""
	fm.focusCurrentEditField()
}

func (fm *FeedManager) focusLocalFeedEdit(feed db.Feed) {
	fm.editTarget = feed.ID
	fm.folderEditTarget = 0
	fm.remoteSettingsEdit = false
	fm.statusMsg = ""
	fm.busy = false
	fm.busyMsg = ""
	fm.titleInput.Reset()
	fm.titleInput.SetValue(feed.Title)
	fm.urlInput.Reset()
	fm.urlInput.SetValue(feed.URL)
	fm.newFolderInput.Reset()
	fm.focusedField = fmFieldBack
	fm.setFolderCursorForID(feed.FolderID)
	fm.focusCurrentEditField()
	fm.mode = fmEdit
	fm.paneFocus = fmPaneDetail
}

func (fm *FeedManager) focusFolderEdit(folder db.Folder) {
	fm.mode = fmFolderEdit
	fm.paneFocus = fmPaneDetail
	fm.folderEditTarget = folder.ID
	fm.titleInput.Reset()
	fm.titleInput.SetValue(folder.Name)
	fm.focusedField = fmFieldBack
	fm.statusMsg = ""
	fm.busy = false
	fm.busyMsg = ""
	fm.focusCurrentEditField()
	if _, idx, ok := folderColorByValue(folder.Color); ok {
		fm.colorCursor = idx
	} else {
		fm.colorCursor = 0
	}
}

func (fm *FeedManager) focusAddFolder() {
	fm.mode = fmFolderEdit
	fm.paneFocus = fmPaneDetail
	fm.folderEditTarget = 0
	fm.titleInput.Reset()
	fm.focusedField = fmFieldBack
	fm.statusMsg = ""
	fm.busy = false
	fm.busyMsg = ""
	fm.focusCurrentEditField()
	fm.colorCursor = 0
}

func (fm *FeedManager) focusImport() {
	fm.mode = fmImport
	fm.paneFocus = fmPaneDetail
	fm.statusMsg = ""
	fm.busy = false
	fm.busyMsg = ""
	fm.importInput.Reset()
	fm.focusedField = fmFieldBack
	fm.focusCurrentEditField()
}

func (fm *FeedManager) focusMove(f db.Feed) {
	fm.mode = fmMove
	fm.paneFocus = fmPaneDetail
	fm.moveTarget = f.ID
	fm.statusMsg = ""
	fm.busy = false
	fm.busyMsg = ""
	fm.focusedField = fmFieldBack
	fm.focusCurrentEditField()
	// Start the picker on the feed's current folder so Enter is a no-op rather than a surprise.
	fm.moveCursor = 0
	for i, folder := range fm.folders {
		if folder.ID == f.FolderID {
			fm.moveCursor = i + 1
			break
		}
	}
}

func (fm *FeedManager) returnToListPane() {
	fm.mode = fmList
	fm.paneFocus = fmPaneList
	fm.remoteSettingsEdit = false
	fm.focusedField = fmFieldBack
	fm.blurEditInputs()
	fm.importInput.Blur()
}

func (fm *FeedManager) leaveEditDetail() {
	if fm.remoteSettingsEdit {
		fm.paneFocus = fmPaneList
		fm.blurEditInputs()
		return
	}
	fm.returnToListPane()
}

func (fm FeedManager) folderOptions() []string {
	options := make([]string, 0, len(fm.folders)+2)
	options = append(options, "(no folder)")
	for _, folder := range fm.folders {
		options = append(options, folder.Name)
	}
	options = append(options, "+ New folder")
	return options
}

// folderOptionsForMove returns the folder list used by the move-feed picker:
// "(no folder)" + existing folders, without the "+ New folder" creation option.
func (fm FeedManager) folderOptionsForMove() []string {
	options := make([]string, 0, len(fm.folders)+1)
	options = append(options, "(no folder)")
	for _, folder := range fm.folders {
		options = append(options, folder.Name)
	}
	return options
}

// moveFolderIDAt returns the folder ID corresponding to a move-picker cursor index.
// 0 is the "(no folder)" sentinel; the rest map 1:1 to fm.folders.
func (fm FeedManager) moveFolderIDAt(idx int) int64 {
	if idx <= 0 || idx > len(fm.folders) {
		return 0
	}
	return fm.folders[idx-1].ID
}

func (fm FeedManager) currentFolderID() int64 {
	if fm.folderCursor <= 0 || fm.folderCursor > len(fm.folders) {
		return 0
	}
	return fm.folders[fm.folderCursor-1].ID
}

func (fm FeedManager) currentColorOption() folderColorOption {
	return folderColorOptions[clamp(fm.colorCursor, 0, len(folderColorOptions)-1)]
}

func (fm FeedManager) displayedColorOption() folderColorOption {
	if fm.mode == fmEdit && !fm.showNewFolder {
		if folder := fm.pickedFolder(); folder != nil {
			if option, _, ok := folderColorByValue(folder.Color); ok {
				return option
			}
		}
	}
	return fm.currentColorOption()
}

func (fm FeedManager) selectedRow() *fmRow {
	rows := fm.managerRows()
	if fm.cursor < 0 || fm.cursor >= len(rows) {
		return nil
	}
	row := rows[fm.cursor]
	return &row
}

func (fm FeedManager) selectedFeedRow() *db.Feed {
	row := fm.selectedRow()
	if row == nil || row.kind != fmRowFeed {
		return nil
	}
	for i := range fm.feeds {
		if fm.feeds[i].ID == row.feedID {
			return &fm.feeds[i]
		}
	}
	return nil
}

func (fm FeedManager) feedIsRemote(feed *db.Feed) bool {
	return feed != nil && feed.ID < 0
}

func (fm *FeedManager) setRemoteBrowseOnlyStatus() {
	fm.statusMsg = "REMOTE FEEDS ARE BROWSE-ONLY"
}

func (fm FeedManager) selectedFolder() *db.Folder {
	if row := fm.selectedRow(); row != nil && row.kind == fmRowFolder {
		for i := range fm.folders {
			if fm.folders[i].ID == row.folderID {
				return &fm.folders[i]
			}
		}
		return nil
	}
	id := fm.currentFolderID()
	if id != 0 {
		for i := range fm.folders {
			if fm.folders[i].ID == id {
				return &fm.folders[i]
			}
		}
	}
	return nil
}

func (fm FeedManager) pickedFolder() *db.Folder {
	id := fm.currentFolderID()
	if id == 0 {
		return nil
	}
	for i := range fm.folders {
		if fm.folders[i].ID == id {
			return &fm.folders[i]
		}
	}
	return nil
}

func (fm *FeedManager) selectFeed(feedID int64) {
	for i, row := range fm.managerRows() {
		if row.kind == fmRowFeed && row.feedID == feedID {
			fm.cursor = i
			return
		}
	}
}

func (fm *FeedManager) selectFolder(folderID int64) {
	for i, row := range fm.managerRows() {
		if row.kind == fmRowFolder && row.folderID == folderID {
			fm.cursor = i
			return
		}
	}
}

func (fm FeedManager) shouldShowColorPicker() bool {
	if fm.mode == fmEdit && !fm.remoteSettingsEdit && fm.editTarget == 0 && fm.addSourceIdx == fmAddSourceGReader {
		return false
	}
	if fm.mode == fmEdit {
		return fm.showNewFolder || fm.pickedFolder() != nil
	}
	if fm.mode == fmFolderEdit {
		return true
	}
	return fm.showNewFolder || fm.selectedFolder() != nil
}

func (fm *FeedManager) syncFolderPicker() {
	maxIdx := max(0, len(fm.folderOptions())-1)
	fm.folderCursor = clamp(fm.folderCursor, 0, maxIdx)
	fm.showNewFolder = fm.folderCursor == len(fm.folderOptions())-1
	if fm.showNewFolder && fm.focusedField == 3 {
		fm.newFolderInput.Focus()
	} else {
		fm.newFolderInput.Blur()
	}
	fm.setColorCursorFromCurrentFolder()
}

func (fm *FeedManager) setFolderCursorForID(folderID int64) {
	fm.folderCursor = 0
	for i, folder := range fm.folders {
		if folder.ID == folderID {
			fm.folderCursor = i + 1
			break
		}
	}
	fm.syncFolderPicker()
	fm.setColorCursorFromCurrentFolder()
}

func (fm *FeedManager) focusCurrentEditField() {
	fm.blurEditInputs()

	switch fm.focusedField {
	case fmFieldBack:
		return
	case 0:
		if fm.mode == fmEdit && !fm.remoteSettingsEdit {
			fm.focusedField = 1
			fm.urlInput.Focus()
			return
		}
		fm.titleInput.Focus()
	case 1:
		if fm.editTarget != 0 {
			fm.titleInput.CursorEnd()
			fm.titleInput.Focus()
		} else {
			fm.urlInput.Focus()
		}
	case 2:
		if fm.editTarget != 0 {
			fm.urlInput.Focus()
		}
	case 3:
		if fm.editTarget != 0 {
			return // folder picker — no text input
		}
		if fm.showNewFolder {
			fm.newFolderInput.Focus()
		} else {
			if fm.mode == fmEdit {
				fm.focusedField = 1
				fm.urlInput.Focus()
			} else {
				fm.focusedField = 0
				fm.titleInput.Focus()
			}
		}
	case 4:
		if fm.editTarget != 0 {
			if fm.showNewFolder {
				fm.newFolderInput.Focus()
			} else {
				fm.focusedField = 1
				fm.titleInput.Focus()
			}
			return
		}
		if !fm.shouldShowColorPicker() {
			if fm.mode == fmEdit {
				fm.focusedField = 1
				fm.urlInput.Focus()
			} else {
				fm.focusedField = 0
				fm.titleInput.Focus()
			}
		}
	case 5:
		// color picker for edit-existing path — no text input to focus
	case fmFieldGReaderURL:
		fm.greaderURLInput.Focus()
	case fmFieldGReaderLogin:
		fm.greaderLoginInput.Focus()
	case fmFieldGReaderPassword:
		fm.greaderPasswordInput.Focus()
	case fmFieldImportPath:
		fm.importInput.Focus()
	}
}

func (fm *FeedManager) blurEditInputs() {
	fm.titleInput.Blur()
	fm.urlInput.Blur()
	fm.newFolderInput.Blur()
	fm.greaderURLInput.Blur()
	fm.greaderLoginInput.Blur()
	fm.greaderPasswordInput.Blur()
	fm.importInput.Blur()
}

func (fm FeedManager) editFieldOrder() []int {
	// Field order is generated from current form state because GReader and new-folder paths expose different controls. -allie
	if fm.remoteSettingsEdit {
		return []int{fmFieldBack, fmFieldGReaderURL, fmFieldGReaderLogin, fmFieldGReaderPassword}
	}
	if fm.editTarget == 0 {
		order := []int{fmFieldBack, fmFieldAddSource}
		if fm.addSourceIdx == fmAddSourceGReader {
			return append(order, 1, fmFieldGReaderURL, fmFieldGReaderLogin, fmFieldGReaderPassword)
		}
		order = append(order, 1, 2)
		if fm.showNewFolder {
			order = append(order, 3)
		}
		if fm.shouldShowColorPicker() {
			order = append(order, 4)
		}
		return order
	}
	order := []int{fmFieldBack, 1, 2, 3}
	if fm.showNewFolder {
		order = append(order, 4)
	}
	if fm.shouldShowColorPicker() {
		order = append(order, 5)
	}
	return order
}

func (fm FeedManager) greaderFeedURL() string {
	return strings.TrimSpace(fm.urlInput.Value())
}

func (fm *FeedManager) advanceEditField() {
	order := fm.editFieldOrder()
	for i, field := range order {
		if field == fm.focusedField {
			fm.focusedField = order[(i+1)%len(order)]
			fm.focusCurrentEditField()
			return
		}
	}
	fm.focusedField = order[0]
	fm.focusCurrentEditField()
}

func (fm FeedManager) isEditTextInputFocused() bool {
	switch fm.focusedField {
	case 0, 1:
		return fm.focusedField == 1 && !fm.remoteSettingsEdit
	case 2:
		return fm.editTarget != 0
	case 3:
		return fm.editTarget == 0 && fm.showNewFolder
	case 4:
		return fm.editTarget != 0 && fm.showNewFolder
	case fmFieldGReaderURL, fmFieldGReaderLogin, fmFieldGReaderPassword:
		return fm.remoteSettingsEdit || (fm.editTarget == 0 && fm.addSourceIdx == fmAddSourceGReader)
	case fmFieldImportPath:
		return fm.mode == fmImport
	default:
		return false
	}
}

func (fm FeedManager) focusedEditTextInputCursorPosition() int {
	switch fm.focusedField {
	case 0:
		return fm.titleInput.Position()
	case 1:
		if fm.editTarget != 0 {
			return fm.titleInput.Position()
		}
		return fm.urlInput.Position()
	case 2:
		if fm.editTarget != 0 {
			return fm.urlInput.Position()
		}
	case 3:
		if fm.editTarget == 0 && fm.showNewFolder {
			return fm.newFolderInput.Position()
		}
	case 4:
		if fm.editTarget != 0 && fm.showNewFolder {
			return fm.newFolderInput.Position()
		}
	case fmFieldGReaderURL:
		if fm.remoteSettingsEdit || (fm.editTarget == 0 && fm.addSourceIdx == fmAddSourceGReader) {
			return fm.greaderURLInput.Position()
		}
	case fmFieldGReaderLogin:
		if fm.remoteSettingsEdit || (fm.editTarget == 0 && fm.addSourceIdx == fmAddSourceGReader) {
			return fm.greaderLoginInput.Position()
		}
	case fmFieldGReaderPassword:
		if fm.remoteSettingsEdit || (fm.editTarget == 0 && fm.addSourceIdx == fmAddSourceGReader) {
			return fm.greaderPasswordInput.Position()
		}
	case fmFieldImportPath:
		if fm.mode == fmImport {
			return fm.importInput.Position()
		}
	}
	return -1
}

func (fm FeedManager) updateFocusedEditInput(msg tea.Msg) (FeedManager, tea.Cmd) {
	var cmd tea.Cmd
	switch {
	case fm.focusedField == 1 && fm.editTarget != 0:
		fm.titleInput, cmd = fm.titleInput.Update(msg)
	case fm.focusedField == 1:
		fm.urlInput, cmd = fm.urlInput.Update(msg)
	case fm.focusedField == 2 && fm.editTarget != 0:
		fm.urlInput, cmd = fm.urlInput.Update(msg)
	case fm.focusedField == 3 && fm.showNewFolder && fm.editTarget == 0:
		fm.newFolderInput, cmd = fm.newFolderInput.Update(msg)
	case fm.focusedField == 4 && fm.showNewFolder && fm.editTarget != 0:
		fm.newFolderInput, cmd = fm.newFolderInput.Update(msg)
	case fm.focusedField == fmFieldGReaderURL:
		fm.greaderURLInput, cmd = fm.greaderURLInput.Update(msg)
	case fm.focusedField == fmFieldGReaderLogin:
		fm.greaderLoginInput, cmd = fm.greaderLoginInput.Update(msg)
	case fm.focusedField == fmFieldGReaderPassword:
		fm.greaderPasswordInput, cmd = fm.greaderPasswordInput.Update(msg)
	case fm.focusedField == fmFieldImportPath:
		fm.importInput, cmd = fm.importInput.Update(msg)
	}
	return fm, cmd
}

func (fm FeedManager) updateFocusedFolderEditInput(msg tea.Msg) (FeedManager, tea.Cmd) {
	var cmd tea.Cmd
	if fm.focusedField == 0 {
		fm.titleInput, cmd = fm.titleInput.Update(msg)
	}
	return fm, cmd
}

func (fm FeedManager) folderEditFieldOrder() []int {
	return []int{fmFieldBack, 0, 4}
}

func (fm FeedManager) advanceFieldInOrder(order []int) FeedManager {
	for i, field := range order {
		if field == fm.focusedField {
			fm.focusedField = order[(i+1)%len(order)]
			fm.focusCurrentEditField()
			return fm
		}
	}
	fm.focusedField = order[0]
	fm.focusCurrentEditField()
	return fm
}

func (fm FeedManager) retreatFieldInOrder(order []int) FeedManager {
	for i, field := range order {
		if field == fm.focusedField {
			fm.focusedField = order[(i+len(order)-1)%len(order)]
			fm.focusCurrentEditField()
			return fm
		}
	}
	fm.focusedField = order[0]
	fm.focusCurrentEditField()
	return fm
}

func (fm *FeedManager) setColorCursorFromCurrentFolder() {
	fm.colorCursor = 0
	var color string
	if fm.showNewFolder {
		color = string(folderColorOptions[0].Color)
	} else if folder := fm.pickedFolder(); folder != nil {
		color = folder.Color
	}
	if _, idx, ok := folderColorByValue(color); ok {
		fm.colorCursor = idx
	}
}

func (fm FeedManager) Update(msg tea.Msg, keys KeyMap) (FeedManager, tea.Cmd, bool) {
	fm.browseFeedID = 0
	fm, cmd := fm.update(msg, keys)
	exit := fm.shouldExit
	fm.shouldExit = false
	return fm, cmd, exit
}

func (fm FeedManager) update(msg tea.Msg, keys KeyMap) (FeedManager, tea.Cmd) {
	// Non-key messages are routed to focused inputs first so cursor blink and textinput internals keep working. -allie
	// Route non-key messages to the focused textinput (cursor blink ticks etc.)
	if _, ok := msg.(tea.KeyMsg); !ok {
		if fm.busy {
			return fm, nil
		}
		switch fm.mode {
		case fmEdit:
			return fm.updateFocusedEditInput(msg)
		case fmFolderEdit:
			return fm.updateFocusedFolderEditInput(msg)
		case fmImport:
			var cmd tea.Cmd
			fm.importInput, cmd = fm.importInput.Update(msg)
			return fm, cmd
		}
		return fm, nil
	}
	key := msg.(tea.KeyMsg)
	switch fm.mode {
	case fmList:
		return fm.updateList(key, keys)
	case fmEdit:
		return fm.updateEdit(key, keys)
	case fmFolderEdit:
		return fm.updateFolderEdit(key, keys)
	case fmImport:
		return fm.updateImport(key, keys)
	case fmConfirmDelete:
		return fm.updateConfirmDelete(key, keys)
	case fmMove:
		return fm.updateMove(key, keys)
	}
	return fm, nil
}

func (fm FeedManager) updateList(msg tea.KeyMsg, keys KeyMap) (FeedManager, tea.Cmd) {
	if fm.busy {
		return fm, nil
	}
	switch {
	case keyMatches(msg, keys.Back):
		fm.shouldExit = true

	case keyMatches(msg, keys.Up):
		if fm.cursor > 0 {
			fm.cursor--
		}

	case keyMatches(msg, keys.Down):
		if fm.cursor < len(fm.managerRows())-1 {
			fm.cursor++
		}

	case keyMatches(msg, keys.Add):
		if !fm.editable() {
			fm.setBrowseOnlyStatus()
			return fm, nil
		}
		fm.focusAdd()

	case keyMatches(msg, keys.AddFolder):
		if !fm.editable() {
			fm.setBrowseOnlyStatus()
			return fm, nil
		}
		fm.focusAddFolder()

	case keyMatches(msg, keys.Edit):
		if !fm.editable() {
			fm.setBrowseOnlyStatus()
			return fm, nil
		}
		if row := fm.selectedRow(); row != nil {
			if row.kind == fmRowFolder {
				if folder := fm.selectedFolder(); folder != nil {
					fm.focusFolderEdit(*folder)
				}
				return fm, nil
			}
			if f := fm.selectedFeedRow(); f != nil {
				if fm.feedIsRemote(f) {
					fm.focusRemoteSettingsEdit(*f)
					return fm, nil
				}
				fm.focusLocalFeedEdit(*f)
			}
		}

	case keyMatches(msg, keys.Enter):
		if f := fm.selectedFeedRow(); f != nil && fm.feedIsRemote(f) {
			fm.browseFeedID = f.ID
			fm.shouldExit = true
			return fm, nil
		}
		if !fm.editable() {
			if f := fm.selectedFeedRow(); f != nil {
				fm.browseFeedID = f.ID
				fm.shouldExit = true
			}
			return fm, nil
		}
		if row := fm.selectedRow(); row != nil {
			if row.kind == fmRowFolder {
				if folder := fm.selectedFolder(); folder != nil {
					fm.focusFolderEdit(*folder)
				}
				return fm, nil
			}
			if f := fm.selectedFeedRow(); f != nil {
				if fm.feedIsRemote(f) {
					fm.browseFeedID = f.ID
					fm.shouldExit = true
					return fm, nil
				}
				fm.focusLocalFeedEdit(*f)
			}
		}

	case keyMatches(msg, keys.Delete):
		if !fm.editable() {
			fm.setBrowseOnlyStatus()
			return fm, nil
		}
		if f := fm.selectedFeedRow(); f != nil && fm.feedIsRemote(f) {
			fm.setRemoteBrowseOnlyStatus()
			return fm, nil
		}
		if fm.selectedRow() != nil {
			fm.mode = fmConfirmDelete
		}

	case keyMatches(msg, keys.Move):
		if !fm.editable() {
			fm.setBrowseOnlyStatus()
			return fm, nil
		}
		if f := fm.selectedFeedRow(); f != nil && !fm.feedIsRemote(f) {
			fm.focusMove(*f)
		}

	case keyMatches(msg, keys.Import):
		if !fm.editable() {
			fm.setBrowseOnlyStatus()
			return fm, nil
		}
		fm.focusImport()

	case keyMatches(msg, keys.Export):
		if !fm.editable() {
			fm.setBrowseOnlyStatus()
			return fm, nil
		}
		return fm, fm.exportCmd()
	}
	return fm, nil
}

func (fm FeedManager) updateEdit(msg tea.KeyMsg, keys KeyMap) (FeedManager, tea.Cmd) {
	if fm.busy {
		return fm, nil
	}
	if fm.paneFocus == fmPaneList {
		switch {
		case keyMatches(msg, keys.Add):
			fm.focusAdd()
		case keyMatches(msg, keys.Cancel):
			fm.shouldExit = true
		case keyMatches(msg, keys.Up):
			if fm.cursor > 0 {
				fm.cursor--
			}
		case keyMatches(msg, keys.Down):
			if fm.cursor < len(fm.managerRows())-1 {
				fm.cursor++
			}
		case keyMatches(msg, keys.Right), keyMatches(msg, keys.Tab), keyMatches(msg, keys.Confirm):
			fm.prefillAddFormFromSelectedRemoteFeed()
			fm.paneFocus = fmPaneDetail
			fm.focusedField = fmFieldBack
			fm.focusCurrentEditField()
		}
		return fm, nil
	}
	if fm.isEditTextInputFocused() && msg.Type == tea.KeyLeft {
		if fm.focusedEditTextInputCursorPosition() == 0 {
			fm.focusedField = fmFieldBack
			fm.focusCurrentEditField()
			return fm, nil
		}
		return fm.updateFocusedEditInput(msg)
	}
	if fm.isEditTextInputFocused() && msg.Type == tea.KeyRunes {
		return fm.updateFocusedEditInput(msg)
	}
	if fm.isEditTextInputFocused() && msg.Type == tea.KeyRight {
		return fm.updateFocusedEditInput(msg)
	}
	switch {
	case keyMatches(msg, keys.Cancel):
		if fm.paneFocus == fmPaneDetail {
			fm.leaveEditDetail()
			return fm, nil
		}
		fm.shouldExit = true

	case keyMatches(msg, keys.Tab), keyMatches(msg, keys.Down):
		fm.advanceEditField()

	case keyMatches(msg, keys.Up):
		order := fm.editFieldOrder()
		for i, field := range order {
			if field == fm.focusedField {
				fm.focusedField = order[(i+len(order)-1)%len(order)]
				fm.focusCurrentEditField()
				return fm, nil
			}
		}
		fm.focusedField = order[0]
		fm.focusCurrentEditField()

	case fm.focusedField == fmFieldBack && (keyMatches(msg, keys.Left) || keyMatches(msg, keys.Enter) || keyMatches(msg, keys.Space)):
		fm.leaveEditDetail()

	case fm.focusedField == fmFieldAddSource && (keyMatches(msg, keys.Left) || keyMatches(msg, keys.Right) || keyMatches(msg, keys.Enter) || keyMatches(msg, keys.Space)):
		fm.addSourceIdx = (fm.addSourceIdx + 1) % len(fmAddSourceLabels)
		fm.folderCursor = 0
		fm.showNewFolder = false
		fm.focusCurrentEditField()

	case fm.focusedField == 2 && fm.editTarget == 0 && keyMatches(msg, keys.Left):
		if fm.folderCursor > 0 {
			fm.folderCursor--
			fm.syncFolderPicker()
			if !fm.showNewFolder && fm.focusedField == 3 {
				fm.focusedField = 2
			}
			fm.focusCurrentEditField()
		}

	case fm.focusedField == 2 && fm.editTarget == 0 && keyMatches(msg, keys.Right):
		if fm.folderCursor < len(fm.folderOptions())-1 {
			fm.folderCursor++
			fm.syncFolderPicker()
			fm.focusCurrentEditField()
		}

	case fm.focusedField == 3 && fm.editTarget != 0 && keyMatches(msg, keys.Left):
		if fm.folderCursor > 0 {
			fm.folderCursor--
			fm.syncFolderPicker()
			if !fm.showNewFolder && fm.focusedField == 4 {
				fm.focusedField = 3
			}
			fm.focusCurrentEditField()
		}

	case fm.focusedField == 3 && fm.editTarget != 0 && keyMatches(msg, keys.Right):
		if fm.folderCursor < len(fm.folderOptions())-1 {
			fm.folderCursor++
			fm.syncFolderPicker()
			fm.focusCurrentEditField()
		}

	case fm.focusedField == 4 && fm.editTarget == 0 && keyMatches(msg, keys.Left):
		if fm.showNewFolder && fm.colorCursor > 0 {
			fm.colorCursor--
		}

	case fm.focusedField == 4 && fm.editTarget == 0 && keyMatches(msg, keys.Right):
		if fm.showNewFolder && fm.colorCursor < len(folderColorOptions)-1 {
			fm.colorCursor++
		}

	case fm.focusedField == 5 && fm.editTarget != 0 && keyMatches(msg, keys.Left):
		if fm.showNewFolder && fm.colorCursor > 0 {
			fm.colorCursor--
		}

	case fm.focusedField == 5 && fm.editTarget != 0 && keyMatches(msg, keys.Right):
		if fm.showNewFolder && fm.colorCursor < len(folderColorOptions)-1 {
			fm.colorCursor++
		}

	case keyMatches(msg, keys.Confirm):
		if fm.focusedField == fmFieldBack {
			fm.leaveEditDetail()
			return fm, nil
		}
		if fm.focusedField == fmFieldAddSource {
			fm.addSourceIdx = (fm.addSourceIdx + 1) % len(fmAddSourceLabels)
			fm.focusCurrentEditField()
			return fm, nil
		}
		if fm.remoteSettingsEdit {
			fm.busyMsg = "SAVING GREADER SETTINGS..."
		} else if fm.editTarget == 0 && fm.addSourceIdx == fmAddSourceGReader {
			if fm.greaderFeedURL() == "" {
				fm.busyMsg = "LOADING GREADER FEEDS..."
			} else {
				fm.busyMsg = "ADDING GREADER FEED..."
			}
		} else if fm.editTarget != 0 {
			fm.busyMsg = "SAVING FEED..."
		} else {
			fm.busyMsg = "ADDING FEED..."
		}
		fm.statusMsg = fm.busyMsg
		fm.busy = true
		return fm, fm.saveCmd()

	default:
		return fm.updateFocusedEditInput(msg)
	}
	return fm, nil
}

func (fm FeedManager) updateFolderEdit(msg tea.KeyMsg, keys KeyMap) (FeedManager, tea.Cmd) {
	if fm.busy {
		return fm, nil
	}
	if fm.focusedField == 0 && msg.Type == tea.KeyRunes {
		return fm.updateFocusedFolderEditInput(msg)
	}
	if fm.focusedField == 0 && msg.Type == tea.KeyLeft {
		if fm.titleInput.Position() == 0 {
			fm.focusedField = fmFieldBack
			fm.focusCurrentEditField()
			return fm, nil
		}
		return fm.updateFocusedFolderEditInput(msg)
	}
	switch {
	case keyMatches(msg, keys.Cancel):
		if fm.paneFocus == fmPaneDetail {
			fm.returnToListPane()
			return fm, nil
		}
		fm.shouldExit = true

	case keyMatches(msg, keys.Tab), keyMatches(msg, keys.Down):
		fm = fm.advanceFieldInOrder(fm.folderEditFieldOrder())

	case keyMatches(msg, keys.Up):
		fm = fm.retreatFieldInOrder(fm.folderEditFieldOrder())

	case fm.focusedField == fmFieldBack && (keyMatches(msg, keys.Left) || keyMatches(msg, keys.Enter) || keyMatches(msg, keys.Space)):
		fm.returnToListPane()

	case fm.focusedField == 4 && keyMatches(msg, keys.Left):
		if fm.colorCursor > 0 {
			fm.colorCursor--
		}

	case fm.focusedField == 4 && keyMatches(msg, keys.Right):
		if fm.colorCursor < len(folderColorOptions)-1 {
			fm.colorCursor++
		}

	case keyMatches(msg, keys.Confirm):
		if fm.focusedField == fmFieldBack {
			fm.returnToListPane()
			return fm, nil
		}
		fm.busyMsg = "SAVING FOLDER..."
		fm.statusMsg = fm.busyMsg
		fm.busy = true
		return fm, fm.saveFolderCmd()

	default:
		return fm.updateFocusedFolderEditInput(msg)
	}
	return fm, nil
}

func (fm FeedManager) updateImport(msg tea.KeyMsg, keys KeyMap) (FeedManager, tea.Cmd) {
	if fm.busy {
		return fm, nil
	}
	if fm.focusedField == fmFieldImportPath && msg.Type == tea.KeyRunes {
		return fm.updateFocusedEditInput(msg)
	}
	if fm.focusedField == fmFieldImportPath && msg.Type == tea.KeyLeft {
		if fm.importInput.Position() == 0 {
			fm.focusedField = fmFieldBack
			fm.focusCurrentEditField()
			return fm, nil
		}
		return fm.updateFocusedEditInput(msg)
	}
	switch {
	case keyMatches(msg, keys.Cancel):
		if fm.paneFocus == fmPaneDetail {
			fm.paneFocus = fmPaneList
			fm.importInput.Blur()
			return fm, nil
		}
		fm.shouldExit = true

	case keyMatches(msg, keys.Tab), keyMatches(msg, keys.Down):
		fm = fm.advanceFieldInOrder([]int{fmFieldBack, fmFieldImportPath})

	case keyMatches(msg, keys.Up):
		fm = fm.retreatFieldInOrder([]int{fmFieldBack, fmFieldImportPath})

	case fm.focusedField == fmFieldBack && (keyMatches(msg, keys.Left) || keyMatches(msg, keys.Enter) || keyMatches(msg, keys.Space)):
		fm.paneFocus = fmPaneList
		fm.blurEditInputs()

	case keyMatches(msg, keys.Confirm):
		if fm.focusedField == fmFieldBack {
			fm.paneFocus = fmPaneList
			fm.blurEditInputs()
			return fm, nil
		}
		path := strings.TrimSpace(fm.importInput.Value())
		fm.statusMsg = "IMPORTING OPML..."
		fm.busyMsg = fm.statusMsg
		fm.busy = true
		return fm, fm.importCmd(path)

	default:
		if fm.focusedField == fmFieldImportPath {
			return fm.updateFocusedEditInput(msg)
		}
	}
	return fm, nil
}

func (fm FeedManager) updateMove(msg tea.KeyMsg, keys KeyMap) (FeedManager, tea.Cmd) {
	if fm.busy {
		return fm, nil
	}
	options := fm.folderOptionsForMove()
	switch {
	case keyMatches(msg, keys.Back), keyMatches(msg, keys.Cancel):
		fm.paneFocus = fmPaneList
		fm.blurEditInputs()
	case keyMatches(msg, keys.Tab):
		fm = fm.advanceFieldInOrder([]int{fmFieldBack, fmFieldMoveFolder})
	case fm.focusedField == fmFieldBack && (keyMatches(msg, keys.Left) || keyMatches(msg, keys.Enter) || keyMatches(msg, keys.Space)):
		fm.paneFocus = fmPaneList
		fm.blurEditInputs()
	case fm.focusedField == fmFieldBack && keyMatches(msg, keys.Down):
		fm.focusedField = fmFieldMoveFolder
		fm.focusCurrentEditField()
	case fm.focusedField == fmFieldMoveFolder && keyMatches(msg, keys.Left):
		fm.focusedField = fmFieldBack
		fm.focusCurrentEditField()
	case fm.focusedField == fmFieldMoveFolder && keyMatches(msg, keys.Up):
		if fm.moveCursor > 0 {
			fm.moveCursor--
		} else {
			fm.focusedField = fmFieldBack
			fm.focusCurrentEditField()
		}
	case fm.focusedField == fmFieldMoveFolder && keyMatches(msg, keys.Down):
		if fm.moveCursor < len(options)-1 {
			fm.moveCursor++
		}
	case keyMatches(msg, keys.Confirm):
		if fm.focusedField == fmFieldBack {
			fm.paneFocus = fmPaneList
			fm.blurEditInputs()
			return fm, nil
		}
		feedID := fm.moveTarget
		folderID := fm.moveFolderIDAt(fm.moveCursor)
		if feedID == 0 {
			fm.paneFocus = fmPaneList
			return fm, nil
		}
		fm.statusMsg = "MOVING FEED..."
		fm.busyMsg = fm.statusMsg
		fm.busy = true
		return fm, fm.moveCmd(feedID, folderID)
	}
	return fm, nil
}

func (fm FeedManager) updateConfirmDelete(msg tea.KeyMsg, keys KeyMap) (FeedManager, tea.Cmd) {
	if fm.busy {
		return fm, nil
	}
	switch {
	case keyMatches(msg, keys.Yes):
		fm.mode = fmList
		if row := fm.selectedRow(); row != nil {
			if row.kind == fmRowFolder {
				fm.statusMsg = "DELETING FOLDER..."
				fm.busyMsg = fm.statusMsg
				fm.busy = true
				return fm, fm.deleteFolderCmd(row.folderID)
			}
			fm.statusMsg = "DELETING FEED..."
			fm.busyMsg = fm.statusMsg
			fm.busy = true
			return fm, fm.deleteCmd(row.feedID)
		}
	case keyMatches(msg, keys.No), keyMatches(msg, keys.Cancel):
		fm.mode = fmList
	}
	return fm, nil
}

// ── Commands ──────────────────────────────────────────────────────────────────

func validateHTTPURL(raw string, fieldName string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid %s: %s", fieldName, raw)
	}
	return nil
}

func (fm *FeedManager) saveCmd() tea.Cmd {
	// Snapshot form values before returning the command; Bubble Tea may keep mutating the manager while it runs. -allie
	rawURL := strings.TrimSpace(fm.urlInput.Value())
	titleValue := strings.TrimSpace(fm.titleInput.Value())
	newFolderName := strings.TrimSpace(fm.newFolderInput.Value())
	greaderURL := strings.TrimSpace(fm.greaderURLInput.Value())
	greaderLogin := strings.TrimSpace(fm.greaderLoginInput.Value())
	greaderPassword := strings.TrimSpace(fm.greaderPasswordInput.Value())
	editTarget := fm.editTarget
	addSourceIdx := fm.addSourceIdx
	remoteSettingsEdit := fm.remoteSettingsEdit
	folderID := fm.currentFolderID()
	createFolder := fm.showNewFolder
	selectedColor := string(fm.currentColorOption().Color)
	database := fm.db

	return func() tea.Msg {
		if remoteSettingsEdit {
			// Editing a remote feed updates shared GReader credentials, not the remote subscription placement. -allie
			if err := validateHTTPURL(greaderURL, "API URL"); err != nil {
				return RemoteFeedAddedMsg{Err: err}
			}
			if strings.TrimSpace(greaderLogin) == "" {
				return RemoteFeedAddedMsg{Err: fmt.Errorf("login is required")}
			}
			if strings.TrimSpace(greaderPassword) == "" {
				return RemoteFeedAddedMsg{Err: fmt.Errorf("password is required")}
			}
			if database != nil {
				if err := database.SetRemoteFeedTitle(editTarget, ""); err != nil {
					return RemoteFeedAddedMsg{Err: err}
				}
			}
			return RemoteFeedAddedMsg{
				SettingsOnly: true,
				Source: config.SourceConfig{
					GReaderURL:      greaderURL,
					GReaderLogin:    greaderLogin,
					GReaderPassword: greaderPassword,
				},
			}
		}

		if editTarget == 0 && addSourceIdx == fmAddSourceGReader {
			// Blank feed URL means "connect and import existing subscriptions"; a filled URL quick-adds one feed. -allie
			if err := validateHTTPURL(greaderURL, "API URL"); err != nil {
				return RemoteFeedAddedMsg{Err: err}
			}
			if strings.TrimSpace(greaderLogin) == "" {
				return RemoteFeedAddedMsg{Err: fmt.Errorf("login is required")}
			}
			if strings.TrimSpace(greaderPassword) == "" {
				return RemoteFeedAddedMsg{Err: fmt.Errorf("password is required")}
			}
			client := greader.New(greaderURL, greaderLogin, greaderPassword)
			if rawURL == "" {
				subscriptions, err := client.ListSubscriptions(context.Background())
				if err != nil {
					return RemoteFeedAddedMsg{Err: err}
				}
				return RemoteFeedAddedMsg{
					Source: config.SourceConfig{
						GReaderURL:      greaderURL,
						GReaderLogin:    greaderLogin,
						GReaderPassword: greaderPassword,
					},
					FeedCount: len(subscriptions),
				}
			}
			if err := validateHTTPURL(rawURL, "feed URL"); err != nil {
				return RemoteFeedAddedMsg{Err: err}
			}
			result, err := client.QuickAdd(context.Background(), rawURL)
			if err != nil {
				return RemoteFeedAddedMsg{Err: err}
			}
			displayTitle := strings.TrimSpace(result.StreamName)
			if displayTitle == "" {
				displayTitle = strings.TrimSpace(result.Query)
			}
			if displayTitle == "" {
				displayTitle = rawURL
			}
			return RemoteFeedAddedMsg{
				Source: config.SourceConfig{
					GReaderURL:      greaderURL,
					GReaderLogin:    greaderLogin,
					GReaderPassword: greaderPassword,
				},
				StreamID: result.StreamID,
				Title:    displayTitle,
			}
		}

		u, err := url.Parse(rawURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return FeedSavedMsg{Err: fmt.Errorf("invalid URL: %s", rawURL)}
		}

		if createFolder && newFolderName != "" {
			folderID, err = database.AddFolder(newFolderName, selectedColor)
			if err != nil {
				return FeedSavedMsg{Err: err}
			}
		} else if createFolder {
			folderID = 0
		}

		if editTarget != 0 {
			if err := database.UpdateFeed(editTarget, titleValue, rawURL); err != nil {
				return FeedSavedMsg{Err: err}
			}
			if err := database.SetFeedFolder(editTarget, folderID); err != nil {
				return FeedSavedMsg{Err: err}
			}
			if folderID != 0 && createFolder {
				if err := database.SetFolderColor(folderID, selectedColor); err != nil {
					return FeedSavedMsg{Err: err}
				}
			}
			f, _ := database.GetFeed(editTarget)
			return FeedSavedMsg{Feed: f}
		}

		// New feed — fetch and parse to get real title (follows auto-discovery)
		parsed, rawURL, err := feed.FetchAndParse(rawURL)
		if err != nil {
			return FeedSavedMsg{Err: err}
		}

		feedTitle := strings.TrimSpace(parsed.Title)
		if feedTitle == "" {
			feedTitle = rawURL
		}

		id, err := database.AddFeed(rawURL, feedTitle, parsed.Description)
		if err != nil {
			return FeedSavedMsg{Err: err}
		}
		if err := database.SetFeedFolder(id, folderID); err != nil {
			return FeedSavedMsg{Err: err}
		}
		if folderID != 0 && createFolder {
			if err := database.SetFolderColor(folderID, selectedColor); err != nil {
				return FeedSavedMsg{Err: err}
			}
		}

		// Upsert articles immediately so the panes aren't empty
		conv := md.NewConverter("", true, nil)
		var firstUpsertErr error
		for _, item := range parsed.Items {
			content, _ := conv.ConvertString(item.Content)
			if err := database.UpsertArticle(db.Article{
				FeedID:      id,
				GUID:        item.GUID,
				Title:       item.Title,
				Link:        item.Link,
				Content:     content,
				PublishedAt: item.PublishedAt,
			}); err != nil && firstUpsertErr == nil {
				firstUpsertErr = err
			}
		}
		if firstUpsertErr != nil {
			fmt.Fprintf(os.Stderr, "initial article upsert failed (feed %d): %v\n", id, firstUpsertErr)
		}

		f, _ := database.GetFeed(id)
		return FeedSavedMsg{Feed: f}
	}
}

func (fm *FeedManager) deleteCmd(feedID int64) tea.Cmd {
	database := fm.db
	return func() tea.Msg {
		err := database.DeleteFeed(feedID)
		return FeedDeletedMsg{FeedID: feedID, Err: err}
	}
}

func (fm *FeedManager) moveCmd(feedID, folderID int64) tea.Cmd {
	database := fm.db
	return func() tea.Msg {
		if err := database.SetFeedFolder(feedID, folderID); err != nil {
			return FeedSavedMsg{Err: err}
		}
		f, err := database.GetFeed(feedID)
		if err != nil {
			return FeedSavedMsg{Err: err}
		}
		return FeedSavedMsg{Feed: f}
	}
}

func (fm *FeedManager) saveFolderCmd() tea.Cmd {
	folderID := fm.folderEditTarget
	name := strings.TrimSpace(fm.titleInput.Value())
	color := string(fm.currentColorOption().Color)
	database := fm.db

	return func() tea.Msg {
		if folderID == 0 {
			id, err := database.AddFolder(name, color)
			if err != nil {
				return FolderSavedMsg{Err: err}
			}
			folders, err := database.ListFolders()
			if err != nil {
				return FolderSavedMsg{Err: err}
			}
			for _, folder := range folders {
				if folder.ID == id {
					return FolderSavedMsg{Folder: folder}
				}
			}
			return FolderSavedMsg{Err: fmt.Errorf("folder %d not found", id)}
		}
		if err := database.RenameFolder(folderID, name); err != nil {
			return FolderSavedMsg{Err: err}
		}
		if err := database.SetFolderColor(folderID, color); err != nil {
			return FolderSavedMsg{Err: err}
		}
		folders, err := database.ListFolders()
		if err != nil {
			return FolderSavedMsg{Err: err}
		}
		for _, folder := range folders {
			if folder.ID == folderID {
				return FolderSavedMsg{Folder: folder}
			}
		}
		return FolderSavedMsg{Err: fmt.Errorf("folder %d not found", folderID)}
	}
}

func (fm *FeedManager) deleteFolderCmd(folderID int64) tea.Cmd {
	database := fm.db
	return func() tea.Msg {
		err := database.DeleteFolder(folderID)
		return FolderDeletedMsg{FolderID: folderID, Err: err}
	}
}

func (fm *FeedManager) importCmd(path string) tea.Cmd {
	database := fm.db
	return func() tea.Msg {
		// Expand ~ if present
		if strings.HasPrefix(path, "~/") {
			home, _ := os.UserHomeDir()
			path = filepath.Join(home, path[2:])
		}

		outlines, err := opml.Import(path)
		if err != nil {
			return OPMLImportedMsg{Err: err}
		}

		count := 0
		for _, o := range outlines {
			feedURL := o.XMLURL
			title := o.Title
			if title == "" {
				title = o.Text
			}
			if _, err := database.AddFeed(feedURL, title, ""); err == nil {
				count++
			}
		}
		return OPMLImportedMsg{Count: count}
	}
}

func (fm *FeedManager) exportCmd() tea.Cmd {
	feeds := fm.feeds
	return func() tea.Msg {
		path, err := opml.ExportPath()
		if err != nil {
			return OPMLExportedMsg{Err: err}
		}

		outlines := make([]opml.Outline, 0, len(feeds))
		for _, f := range feeds {
			outlines = append(outlines, opml.Outline{
				Text:   f.Title,
				Title:  f.Title,
				Type:   "rss",
				XMLURL: f.URL,
			})
		}

		err = opml.Export(outlines, path)
		return OPMLExportedMsg{Path: path, Err: err}
	}
}

// ── View ──────────────────────────────────────────────────────────────────────

func (fm *FeedManager) View(width, height int, styles Styles, icons bool) string {
	if styles.PlainUI {
		fm.greaderPasswordInput.EchoCharacter = '*'
	} else {
		fm.greaderPasswordInput.EchoCharacter = '●'
	}
	contentW := min(width, 74)
	chrome := newManagerChrome(contentW, styles.Theme, styles.PlainUI)
	header := renderManagerHeader("MANAGER", contentW, chrome)
	status := ""
	hints := ""

	if fm.mode != fmList {
		hints = fm.viewHints(contentW, chrome)
	}

	if fm.statusMsg != "" {
		status = chrome.statusBar.Render(truncate(strings.ToUpper(strings.ReplaceAll(fm.statusMsg, "\n", " ")), max(1, contentW-4)))
	}

	statusH, hintsH := 0, 0
	if status != "" {
		statusH = lipgloss.Height(status)
	}
	if hints != "" {
		hintsH = lipgloss.Height(hints)
	}
	spacerH := 1
	bodyH := max(1, height-lipgloss.Height(header)-spacerH-statusH-hintsH)
	if fm.mode == fmList {
		bodyH = max(1, bodyH-lipgloss.Height(fm.viewListActions(contentW, chrome)))
	}
	body := fm.viewSplit(contentW, bodyH, chrome, styles, icons)

	spacer := lipgloss.NewStyle().Background(chrome.baseBg).Width(contentW).Render("")
	parts := []string{header, spacer, body}
	if status != "" {
		parts = append(parts, status)
	}
	if hints != "" && fm.mode != fmList {
		parts = append(parts, hints)
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (fm FeedManager) viewSplit(width, height int, chrome managerChrome, styles Styles, icons bool) string {
	leftW := clamp(width/4, 18, 20)
	if width-leftW-1 < 24 {
		leftW = max(18, width-25)
	}
	rightW := max(18, width-leftW-1)
	left := fm.viewListPane(leftW, height, chrome, styles, icons)
	right := fm.viewWorkspacePane(rightW, height, chrome, styles)
	separator := lipgloss.NewStyle().Background(chrome.baseBg).Render(" ")
	main := lipgloss.JoinHorizontal(lipgloss.Top, left, separator, right)
	if fm.mode == fmList {
		return lipgloss.JoinVertical(lipgloss.Left, main, fm.viewListActions(width, chrome))
	}
	return main
}

func (fm FeedManager) viewListPane(width, height int, chrome managerChrome, styles Styles, icons bool) string {
	rows := fm.managerRows()
	if len(rows) == 0 {
		body := renderManagerPanel(width, fm.emptyStateMessage(), chrome)
		section := clampView(renderManagerPaneSection(fm.listSectionTitle(), body, fm.listPaneFocused(), chrome), width, height, chrome.baseBg)
		return lipgloss.NewStyle().Width(width).Height(height).Background(chrome.baseBg).Render(section)
	}
	listRows := make([]string, 0, len(rows))
	for i, row := range rows {
		switch row.kind {
		case fmRowFolder:
			folder := fm.folderByID(row.folderID)
			if folder == nil {
				continue
			}
			label := strings.ToUpper(truncate(unescapeDisplayText(folder.Name), max(8, width-6)))
			listRows = append(listRows, renderManagerFolderRow(width, label, folder.Color, fm.collapsedFolders[folder.ID], chrome, styles, i == fm.cursor, icons))
		case fmRowFeed:
			feed := fm.feedByID(row.feedID)
			if feed == nil {
				continue
			}
			title := strings.ToUpper(truncate(unescapeDisplayText(feed.Title), max(8, width-6)))
			nested := false
			last := false
			if feed.FolderID != 0 {
				if fm.folderByID(feed.FolderID) != nil {
					nested = true
					last = true
					// Look ahead for a sibling feed in the same folder; if none, this is the last child.
					for j := i + 1; j < len(rows); j++ {
						if rows[j].kind != fmRowFeed {
							break
						}
						if sibling := fm.feedByID(rows[j].feedID); sibling != nil && sibling.FolderID == feed.FolderID {
							last = false
							break
						}
					}
				}
			}
			if i == fm.cursor {
				label := managerFeedMarker(chrome, icons, nested, last) + title
				listRows = append(listRows, renderManagerSelectedRow(width, label, chrome, styles))
				continue
			}
			color := ""
			if folder := fm.folderByID(feed.FolderID); folder != nil {
				color = folder.Color
			}
			listRows = append(listRows, renderManagerFeedRow(width, title, color, chrome, styles, icons, nested, last))
		}
	}
	section := clampView(renderManagerPaneSection(fm.listSectionTitle(), lipgloss.JoinVertical(lipgloss.Left, listRows...), fm.listPaneFocused(), chrome), width, height, chrome.baseBg)
	return lipgloss.NewStyle().Width(width).Height(height).Background(chrome.baseBg).Render(section)
}

func (fm FeedManager) viewListDetails(width int, chrome managerChrome) string {
	if row := fm.selectedRow(); row != nil {
		switch row.kind {
		case fmRowFolder:
			if folder := fm.folderByID(row.folderID); folder != nil {
				colorName := "THEME DEFAULT"
				if option, _, ok := folderColorByValue(folder.Color); ok {
					colorName = strings.ToUpper(option.Name)
				} else if folder.Color != "" {
					colorName = strings.ToUpper(folder.Color)
				}
				body := strings.ToUpper(truncate(unescapeDisplayText(folder.Name), max(8, width-4))) + "\n" +
					fmt.Sprintf("FEEDS: %d\nUNREAD: %d\nCOLOR: %s", fm.folderFeedCount(folder.ID), fm.folderUnreadCount(folder.ID), colorName)
				return renderManagerPanel(width, body, chrome)
			}
		case fmRowFeed:
			if feed := fm.feedByID(row.feedID); feed != nil {
				sourceLine := renderManagerSourceLine(width, strings.ToUpper(truncate(feed.URL, max(8, width-4))), chrome)
				if fm.feedIsRemote(feed) {
					apiURL := strings.TrimSpace(fm.greaderURLInput.Value())
					if apiURL == "" {
						apiURL = "not set"
					}
					login := strings.TrimSpace(fm.greaderLoginInput.Value())
					if login == "" {
						login = "not set"
					}
					password := maskedPreview(strings.TrimSpace(fm.greaderPasswordInput.Value()), 12, chrome.plainUI)
					if password == "" {
						password = "not set"
					}
					lines := []string{
						sourceLine,
						"SOURCE: GOOGLE READER",
					}
					if folder := fm.folderByID(feed.FolderID); folder != nil {
						lines = append(lines, "FOLDER: "+strings.ToUpper(unescapeDisplayText(folder.Name)))
					}
					lines = append(lines,
						"API URL: "+strings.ToUpper(apiURL),
						"LOGIN: "+strings.ToUpper(login),
						"PASSWORD: "+password,
					)
					if category := strings.TrimSpace(feed.Description); category != "" {
						lines = append(lines, "CATEGORY: "+strings.ToUpper(unescapeDisplayText(category)))
					}
					return renderManagerPanel(width, strings.Join(lines, "\n"), chrome)
				}
				if !fm.editable() {
					lines := []string{sourceLine, "SOURCE: " + strings.ToUpper(fm.sourceName())}
					if category := strings.TrimSpace(feed.Description); category != "" {
						lines = append(lines, "CATEGORY: "+strings.ToUpper(unescapeDisplayText(category)))
					}
					return renderManagerPanel(width, strings.Join(lines, "\n"), chrome)
				}
				if folder := fm.folderByID(feed.FolderID); folder != nil {
					return renderManagerPanel(width, sourceLine+"\nFOLDER: "+strings.ToUpper(unescapeDisplayText(folder.Name)), chrome)
				}
				return renderManagerPanel(width, sourceLine+"\nFOLDER: UNCATEGORIZED", chrome)
			}
		}
	}
	return renderManagerPanel(width, "NO SELECTION", chrome)
}

func (fm FeedManager) viewWorkspacePane(width, height int, chrome managerChrome, styles Styles) string {
	title := fm.listModeTitle()
	body := fm.viewListDetails(width, chrome)
	switch fm.mode {
	case fmEdit:
		title = "ADD FEED"
		if fm.remoteSettingsEdit {
			title = "GREADER SETTINGS"
		} else if fm.editTarget != 0 {
			title = "EDIT FEED"
		}
		if !fm.listPaneFocused() {
			body = fm.viewEdit(width, height, chrome, styles)
		}
	case fmFolderEdit:
		title = "ADD FOLDER"
		if fm.folderEditTarget != 0 {
			title = "EDIT FOLDER"
		}
		body = fm.viewFolderEdit(width, height, chrome, styles)
	case fmImport:
		title = "IMPORT OPML"
		body = fm.viewImport(width, height, chrome)
	case fmConfirmDelete:
		title = fm.confirmDeleteTitle()
		body = fm.viewConfirmDelete(width, height, chrome)
	case fmMove:
		title = "MOVE FEED TO FOLDER"
		body = fm.viewMove(width, height, chrome, styles)
	}
	if fm.mode != fmList && fm.mode != fmConfirmDelete {
		body = fm.renderWorkspaceBodyWithBack(width, body, chrome)
	}
	section := clampView(renderManagerWorkspaceSection(title, body, !fm.listPaneFocused(), chrome), width, height, chrome.baseBg)
	return lipgloss.NewStyle().Width(width).Height(height).Background(chrome.baseBg).Render(section)
}

func (fm FeedManager) viewListActions(width int, chrome managerChrome) string {
	if !fm.editable() {
		sourceName := fm.sourceName()
		if sourceName == "" {
			sourceName = "remote"
		}
		return renderManagerActionGroups(width, chrome,
			[]string{"enter", "browse", "esc", "back"},
			[]string{"mode", "browse-only", "source", sourceName},
		)
	}
	return renderManagerActionGroups(width, chrome,
		[]string{"a", "add feed", "n", "add folder", "e", "edit", "v", "move", "d", "delete"},
		[]string{"i", "import", "x", "export", "esc", "back"},
	)
}

func renderManagerInset(spaces int, s string) string {
	if spaces <= 0 {
		return s
	}
	return strings.Repeat(" ", spaces) + s
}

func (fm FeedManager) feedByID(id int64) *db.Feed {
	for i := range fm.feeds {
		if fm.feeds[i].ID == id {
			return &fm.feeds[i]
		}
	}
	return nil
}

func (fm FeedManager) folderByID(id int64) *db.Folder {
	for i := range fm.folders {
		if fm.folders[i].ID == id {
			return &fm.folders[i]
		}
	}
	return nil
}

func (fm FeedManager) addSourceLabel() string {
	if fm.addSourceIdx < 0 || fm.addSourceIdx >= len(fmAddSourceLabels) {
		return fmAddSourceLabels[0]
	}
	return fmAddSourceLabels[fm.addSourceIdx]
}

func (fm FeedManager) viewEdit(width, height int, chrome managerChrome, styles Styles) string {
	fieldW := max(1, width-2)
	detailFocused := !fm.listPaneFocused()
	contentRows := []string{}
	if fm.editTarget == 0 && !fm.remoteSettingsEdit {
		contentRows = append(contentRows,
			renderManagerSection("Source", renderManagerPicker(fieldW, fm.addSourceLabel(), detailFocused && fm.focusedField == fmFieldAddSource, chrome, styles), chrome, detailFocused && fm.focusedField == fmFieldAddSource),
		)
	}
	if fm.remoteSettingsEdit {
		feedNameLine := strings.ToUpper(truncate(strings.TrimSpace(fm.titleInput.Value()), max(8, fieldW-4)))
		if feedNameLine == "" {
			feedNameLine = "—"
		}
		contentRows = append(contentRows,
			renderManagerSection("Name", renderManagerReadOnlyLine(fieldW, feedNameLine, chrome), chrome, false),
			renderManagerSection("Feed URL", renderManagerReadOnlyLine(fieldW, strings.ToUpper(truncate(fm.urlInput.Value(), max(8, fieldW-4))), chrome), chrome, false),
		)
		contentRows = append(contentRows,
			renderManagerSection("API URL", renderTextInput(fm.greaderURLInput, fieldW, detailFocused && fm.focusedField == fmFieldGReaderURL, false, chrome), chrome, detailFocused && fm.focusedField == fmFieldGReaderURL),
			renderManagerSection("Login", renderTextInput(fm.greaderLoginInput, fieldW, detailFocused && fm.focusedField == fmFieldGReaderLogin, false, chrome), chrome, detailFocused && fm.focusedField == fmFieldGReaderLogin),
			renderManagerSection("Password", renderTextInput(fm.greaderPasswordInput, fieldW, detailFocused && fm.focusedField == fmFieldGReaderPassword, true, chrome), chrome, detailFocused && fm.focusedField == fmFieldGReaderPassword),
		)
	} else if fm.editTarget == 0 && fm.addSourceIdx == fmAddSourceGReader {
		contentRows = append(contentRows,
			renderManagerSection("Name", renderManagerReadOnlyLine(fieldW, strings.ToUpper(truncate("Pulled from the feed when added.", max(8, fieldW-4))), chrome), chrome, false),
			renderManagerSection("URL (optional)", renderTextInput(fm.urlInput, fieldW, detailFocused && fm.focusedField == 1, false, chrome), chrome, detailFocused && fm.focusedField == 1),
			renderManagerSection("API URL", renderTextInput(fm.greaderURLInput, fieldW, detailFocused && fm.focusedField == fmFieldGReaderURL, false, chrome), chrome, detailFocused && fm.focusedField == fmFieldGReaderURL),
			renderManagerSection("Login", renderTextInput(fm.greaderLoginInput, fieldW, detailFocused && fm.focusedField == fmFieldGReaderLogin, false, chrome), chrome, detailFocused && fm.focusedField == fmFieldGReaderLogin),
			renderManagerSection("Password", renderTextInput(fm.greaderPasswordInput, fieldW, detailFocused && fm.focusedField == fmFieldGReaderPassword, true, chrome), chrome, detailFocused && fm.focusedField == fmFieldGReaderPassword),
		)
	} else if fm.editTarget != 0 {
		contentRows = append(contentRows,
			renderManagerSection("Name", renderTextInput(fm.titleInput, fieldW, detailFocused && fm.focusedField == 1, false, chrome), chrome, detailFocused && fm.focusedField == 1),
			renderManagerSection("URL", renderTextInput(fm.urlInput, fieldW, detailFocused && fm.focusedField == 2, false, chrome), chrome, detailFocused && fm.focusedField == 2),
			renderManagerSection("Folder", renderManagerPicker(fieldW, fm.folderOptions()[fm.folderCursor], detailFocused && fm.focusedField == 3, chrome, styles), chrome, detailFocused && fm.focusedField == 3),
		)
		if fm.showNewFolder {
			contentRows = append(contentRows,
				renderManagerSection("New", renderTextInput(fm.newFolderInput, fieldW, detailFocused && fm.focusedField == 4, false, chrome), chrome, detailFocused && fm.focusedField == 4),
			)
		}
		if fm.shouldShowColorPicker() {
			contentRows = append(contentRows,
				renderManagerSection("Color", renderManagerColorPicker(fieldW, fm.displayedColorOption(), detailFocused && fm.focusedField == 5, chrome, styles), chrome, detailFocused && fm.focusedField == 5),
			)
		}
	} else {
		contentRows = append(contentRows,
			renderManagerSection("Name", renderManagerReadOnlyLine(fieldW, strings.ToUpper(truncate("Pulled from the feed when refreshed.", max(8, fieldW-4))), chrome), chrome, false),
			renderManagerSection("URL", renderTextInput(fm.urlInput, fieldW, detailFocused && fm.focusedField == 1, false, chrome), chrome, detailFocused && fm.focusedField == 1),
			renderManagerSection("Folder", renderManagerPicker(fieldW, fm.folderOptions()[fm.folderCursor], detailFocused && fm.focusedField == 2, chrome, styles), chrome, detailFocused && fm.focusedField == 2),
		)
		if fm.showNewFolder {
			contentRows = append(contentRows,
				renderManagerSection("New", renderTextInput(fm.newFolderInput, fieldW, detailFocused && fm.focusedField == 3, false, chrome), chrome, detailFocused && fm.focusedField == 3),
			)
		}
		if fm.shouldShowColorPicker() {
			contentRows = append(contentRows,
				renderManagerSection("Color", renderManagerColorPicker(fieldW, fm.displayedColorOption(), detailFocused && fm.focusedField == 4, chrome, styles), chrome, detailFocused && fm.focusedField == 4),
			)
		}
	}
	return renderManagerDetailColumn(width, contentRows, chrome)
}

func (fm FeedManager) viewFolderEdit(width, height int, chrome managerChrome, styles Styles) string {
	fieldW := max(1, width-2)
	detailFocused := !fm.listPaneFocused()
	return renderManagerDetailColumn(width, []string{
		renderManagerSection("Name", renderTextInput(fm.titleInput, fieldW, detailFocused && fm.focusedField == 0, false, chrome), chrome, detailFocused && fm.focusedField == 0),
		renderManagerSection("Color", renderManagerColorPicker(fieldW, fm.currentColorOption(), detailFocused && fm.focusedField == 4, chrome, styles), chrome, detailFocused && fm.focusedField == 4),
	}, chrome)
}

func (fm FeedManager) viewImport(width, height int, chrome managerChrome) string {
	fieldW := max(1, width-2)
	detailFocused := !fm.listPaneFocused()
	return renderManagerDetailColumn(width, []string{
		renderManagerSection("PATH", renderTextInput(fm.importInput, fieldW, detailFocused && fm.focusedField == fmFieldImportPath, false, chrome), chrome, detailFocused && fm.focusedField == fmFieldImportPath),
	}, chrome)
}

func (fm FeedManager) viewConfirmDelete(width, height int, chrome managerChrome) string {
	row := fm.selectedRow()
	if row == nil {
		return renderManagerPanel(width, "NO SELECTION", chrome)
	}
	name := ""
	warning := ""
	if row.kind == fmRowFolder {
		if folder := fm.folderByID(row.folderID); folder != nil {
			name = unescapeDisplayText(folder.Name)
			warning = "FEEDS IN THIS FOLDER WILL BE KEPT AND MOVED TO UNCATEGORIZED."
		}
	} else if feed := fm.feedByID(row.feedID); feed != nil {
		name = unescapeDisplayText(feed.Title)
		warning = "ALL ARTICLES FROM THIS FEED WILL BE REMOVED."
	}
	fieldW := max(1, width-2)
	warningBlock := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.errorFg).Bold(true).Width(max(12, fieldW)).Render("WARNING"),
		chrome.body.Width(max(12, fieldW)).Render(warning),
	)
	return renderManagerDetailColumn(width, []string{
		renderManagerSection("TARGET", renderManagerPanel(fieldW, strings.ToUpper(truncate(name, max(1, fieldW-4))), chrome), chrome, false),
		warningBlock,
	}, chrome)
}

func (fm FeedManager) viewMove(width, height int, chrome managerChrome, styles Styles) string {
	fieldW := max(1, width-2)
	retro := config.IsRetroTerminalTheme(string(styles.Theme.Name))

	// ── Feed panel: title tinted by the feed's current folder color ───────────
	feedName := ""
	feedColor := ""
	if f := fm.feedByID(fm.moveTarget); f != nil {
		feedName = unescapeDisplayText(f.Title)
		if folder := fm.folderByID(f.FolderID); folder != nil {
			feedColor = folder.Color
		}
	}
	if retro {
		feedColor = ""
	}
	feedTextW := max(1, fieldW-4)
	feedTextStyle := lipgloss.NewStyle().Background(chrome.surfaceBg).Foreground(chrome.text)
	if feedColor != "" {
		feedTextStyle = feedTextStyle.Foreground(accentReadableOn(lipgloss.Color(feedColor), chrome.surfaceBg, 3))
	}
	feedLabel := feedTextStyle.Render(strings.ToUpper(truncate(feedName, feedTextW)))

	// ── Folder picker rows: each option colored by its folder's accent ────────
	options := fm.folderOptionsForMove()
	listRows := make([]string, 0, len(options))
	for i, label := range options {
		displayed := strings.ToUpper(truncate(label, max(4, fieldW-4)))
		if i == fm.moveCursor {
			listRows = append(listRows, renderManagerSelectedRow(fieldW, displayed, chrome, styles))
			continue
		}
		fg := chrome.muted // default for "(no folder)"
		if i > 0 {
			folder := fm.folders[i-1]
			fg = chrome.text
			if !retro && folder.Color != "" {
				fg = accentReadableOn(lipgloss.Color(folder.Color), chrome.baseBg, 3)
			}
		}
		row := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Padding(0, 1).Width(fieldW).Render(displayed)
		listRows = append(listRows, row)
	}
	folderList := lipgloss.JoinVertical(lipgloss.Left, listRows...)
	detailFocused := !fm.listPaneFocused()
	return renderManagerDetailColumn(width, []string{
		renderManagerSection("FEED", renderManagerPanel(fieldW, feedLabel, chrome), chrome, false),
		renderManagerSection("FOLDER", folderList, chrome, detailFocused && fm.focusedField == fmFieldMoveFolder),
	}, chrome)
}

func (fm FeedManager) viewHints(width int, chrome managerChrome) string {
	if fm.busy {
		return renderManagerActions(width, chrome, "working", strings.ToLower(fm.busyMsg))
	}
	switch fm.mode {
	case fmEdit:
		if fm.paneFocus == fmPaneList {
			return renderManagerActions(width, chrome,
				"a", "new feed",
				"↑/↓", "browse list",
				"enter", "edit form",
				"esc", "close",
			)
		}
		enterLabel := "save feed"
		pickLabel := "pick"
		if fm.remoteSettingsEdit {
			enterLabel = "save settings"
		} else if fm.editTarget == 0 {
			enterLabel = "add feed"
			if fm.addSourceIdx == fmAddSourceGReader && fm.greaderFeedURL() == "" {
				enterLabel = "load feeds"
			}
		}
		if fm.focusedField == fmFieldAddSource {
			enterLabel = "toggle source"
			pickLabel = "toggle"
		} else if fm.focusedField == fmFieldBack {
			enterLabel = "sections"
			pickLabel = "back"
		}
		return renderManagerActions(width, chrome,
			"tab", "next field",
			"←/→", pickLabel,
			"enter", enterLabel,
			"esc", "list pane",
		)
	case fmFolderEdit:
		if fm.paneFocus == fmPaneList {
			return renderManagerActions(width, chrome,
				"tab", "next field",
				"←/→", "color",
				"enter", "save folder",
				"esc", "close",
			)
		}
		enterLabel := "save folder"
		pickLabel := "color"
		if fm.focusedField == fmFieldBack {
			enterLabel = "sections"
			pickLabel = "back"
		}
		return renderManagerActions(width, chrome,
			"tab", "next field",
			"←/→", pickLabel,
			"enter", enterLabel,
			"esc", "list pane",
		)
	case fmImport:
		if fm.paneFocus == fmPaneList {
			return renderManagerActions(width, chrome,
				"enter", "import",
				"esc", "close",
			)
		}
		enterLabel := "import"
		if fm.focusedField == fmFieldBack {
			enterLabel = "sections"
		}
		return renderManagerActions(width, chrome,
			"tab", "path",
			"enter", enterLabel,
			"esc", "list pane",
		)
	case fmConfirmDelete:
		return renderManagerActions(width, chrome,
			"y", "confirm",
			"esc", "cancel",
		)
	case fmMove:
		enterLabel := "move"
		pickLabel := "pick folder"
		if fm.focusedField == fmFieldBack {
			enterLabel = "sections"
			pickLabel = "back"
		}
		return renderManagerActions(width, chrome,
			"↑/↓", pickLabel,
			"enter", enterLabel,
			"esc", "cancel",
		)
	default:
		return ""
	}
}

type managerChrome struct {
	baseBg             lipgloss.Color
	surfaceBg          lipgloss.Color
	fieldBg            lipgloss.Color
	accent             lipgloss.Color
	accentFg           lipgloss.Color
	highlight          lipgloss.Color
	highlightFg        lipgloss.Color
	border             lipgloss.Color
	text               lipgloss.Color
	muted              lipgloss.Color
	errorFg            lipgloss.Color
	successFg          lipgloss.Color
	pendingFg          lipgloss.Color
	header             lipgloss.Style
	sectionLabel       lipgloss.Style
	sectionLabelActive lipgloss.Style
	body               lipgloss.Style
	panel              lipgloss.Style
	panelSelected      lipgloss.Style
	key                lipgloss.Style
	keyLabel           lipgloss.Style
	statusBar          lipgloss.Style
	plainUI            bool
}

func (c managerChrome) pickerChevronLeft() string {
	if c.plainUI {
		return "< "
	}
	return "◀ "
}

func (c managerChrome) pickerChevronRight() string {
	if c.plainUI {
		return " >"
	}
	return " ▶"
}

func newManagerChrome(width int, t Theme, plainUI bool) managerChrome {
	baseBg := modalSurface(t)
	surfaceDelta := 0.04
	fieldDelta := 0.08
	if !isDark(baseBg) {
		surfaceDelta = -surfaceDelta
		fieldDelta = -fieldDelta
	}
	surfaceBg := adjustLightness(baseBg, surfaceDelta)
	fieldBg := adjustLightness(baseBg, fieldDelta)
	accent := t.BorderFocus
	if accent == "" {
		accent = t.OverlayBorder
	}
	if accent == "" {
		accent = t.Border
	}
	accentFg := contrastFg(accent)
	text := readableText(t.Fg, baseBg, 4.5)
	muted := mutedText(text, baseBg)
	// Softer than accent: form focus / selection (accent stays for key badges and primary actions).
	highlight := accent
	if isDark(baseBg) {
		highlight = adjustLightness(accent, -0.16)
	} else {
		highlight = adjustLightness(accent, 0.12)
	}
	highlightFg := contrastFg(highlight)
	border := t.OverlayBorder
	if border == "" {
		border = t.Border
	}
	if border == "" {
		border = accent
	}
	errorFg := t.Error
	if errorFg == "" {
		errorFg = accent
	}
	successFg := t.Unread
	if successFg == "" {
		successFg = accent
	}
	pendingFg := t.BorderFocus
	if pendingFg == "" {
		pendingFg = accent
	}

	return managerChrome{
		baseBg:      baseBg,
		surfaceBg:   surfaceBg,
		fieldBg:     fieldBg,
		plainUI:     plainUI,
		accent:      accent,
		accentFg:    accentFg,
		highlight:   highlight,
		highlightFg: highlightFg,
		border:      border,
		text:        text,
		muted:       muted,
		errorFg:     errorFg,
		successFg:   successFg,
		pendingFg:   pendingFg,
		header: lipgloss.NewStyle().
			Width(width).
			Background(accent).
			Foreground(accentFg).
			Bold(true).
			Padding(0, 1),
		sectionLabel: lipgloss.NewStyle().
			Background(baseBg).
			Foreground(muted).
			Padding(0, 1).
			Bold(true),
		sectionLabelActive: lipgloss.NewStyle().
			Background(highlight).
			Foreground(highlightFg).
			Padding(0, 1).
			Bold(true),
		body: lipgloss.NewStyle().
			Background(baseBg).
			Foreground(text),
		panel: lipgloss.NewStyle().
			Width(max(1, width-4)).
			Background(surfaceBg).
			Foreground(text).
			Border(lipPaneBorder(plainUI)).
			BorderForeground(border).
			BorderBackground(surfaceBg).
			Padding(0, 1),
		panelSelected: lipgloss.NewStyle().
			Background(highlight).
			Foreground(highlightFg).
			Bold(true).
			Padding(0, 1),
		key: lipgloss.NewStyle().
			Background(accent).
			Foreground(accentFg).
			Bold(true).
			Padding(0, 1),
		keyLabel: lipgloss.NewStyle().
			Background(baseBg).
			Foreground(muted),
		statusBar: lipgloss.NewStyle().
			Width(width).
			Background(surfaceBg).
			Foreground(readableText(accent, surfaceBg, 3)).
			Border(lipPaneBorder(plainUI), true, false, false, false).
			BorderForeground(border).
			Padding(0, 1),
	}
}

func renderManagerHeader(title string, width int, chrome managerChrome) string {
	gap := max(0, width-lipgloss.Width(title)-2)
	return chrome.header.Render(title + strings.Repeat(" ", gap))
}

func renderManagerSection(label, body string, chrome managerChrome, labelActive bool) string {
	w := lipgloss.Width(body)
	style := chrome.sectionLabel
	if labelActive {
		style = chrome.sectionLabelActive
	}
	styledLabel := style.Width(w).Render(label)
	return lipgloss.JoinVertical(lipgloss.Left, styledLabel, body)
}

func renderManagerPaneSection(label, body string, focused bool, chrome managerChrome) string {
	w := lipgloss.Width(body)
	style := chrome.sectionLabel
	if focused {
		style = chrome.sectionLabelActive
	}
	styledLabel := style.Width(w).Render(label)
	return lipgloss.JoinVertical(lipgloss.Left, styledLabel, body)
}

func renderManagerWorkspaceSection(label, body string, focused bool, chrome managerChrome) string {
	w := lipgloss.Width(body)
	style := chrome.sectionLabel
	if focused {
		style = chrome.sectionLabelActive
	}
	styledLabel := style.Width(w).Render(label)
	headingGap := lipgloss.NewStyle().Background(chrome.baseBg).Width(w).Render("")
	return lipgloss.JoinVertical(lipgloss.Left, styledLabel, headingGap, body)
}

func (fm FeedManager) renderWorkspaceBodyWithBack(width int, body string, chrome managerChrome) string {
	rows := []string{
		renderManagerBackLinkRow(fm.focusedField == fmFieldBack && !fm.listPaneFocused(), width, chrome),
		body,
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func renderManagerBackLinkRow(focused bool, width int, chrome managerChrome) string {
	label := "← Back to sections"
	style := chrome.body.Foreground(chrome.muted)
	if focused {
		style = chrome.sectionLabelActive
	}
	return lipgloss.NewStyle().
		Background(chrome.baseBg).
		Width(width).
		PaddingLeft(2).
		Render(style.Width(max(1, width-2)).Render(label))
}

func renderManagerDetailColumn(width int, sections []string, chrome managerChrome) string {
	innerW := max(1, width-2)
	gap := lipgloss.NewStyle().Background(chrome.baseBg).Width(innerW).Render("")
	rows := make([]string, 0, max(0, len(sections)*2-1))
	for _, section := range sections {
		if section == "" {
			continue
		}
		if len(rows) > 0 {
			rows = append(rows, gap)
		}
		rows = append(rows, clampView(section, innerW, lipgloss.Height(section), chrome.baseBg))
	}
	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return lipgloss.NewStyle().
		Background(chrome.baseBg).
		Width(width).
		PaddingLeft(2).
		Render(clampView(content, innerW, lipgloss.Height(content), chrome.baseBg))
}

func renderManagerPanel(width int, content string, chrome managerChrome) string {
	// Panel rows are padded before boxing so ANSI resets inside content cannot expose terminal background at the edge. -allie
	panelW := max(1, width-4) // total width incl. padding, excl. border
	textW := max(1, panelW-2) // subtract Padding(0,1) on each side
	bgStyle := lipgloss.NewStyle().Background(chrome.surfaceBg)
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		l = ansi.Truncate(l, textW, "")
		if pad := textW - lipgloss.Width(l); pad > 0 {
			l += bgStyle.Render(strings.Repeat(" ", pad))
		}
		// No reset — preserve bg state so panel's right padding/border inherit surfaceBg
		lines[i] = l
	}
	panel := chrome.panel.Width(panelW).Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().Width(width).Background(chrome.baseBg).Render(panel)
}

func renderManagerReadOnlyLine(width int, content string, chrome managerChrome) string {
	w := max(1, width)
	return chrome.body.
		Foreground(chrome.muted).
		Width(w).
		Render("  " + truncate(content, max(1, w-2)))
}

func renderTextInput(input textinput.Model, width int, focused bool, compactSecretPreview bool, chrome managerChrome) string {
	// Input rendering owns every cell's background because bubbles textinput emits plain spaces and ANSI resets. -allie
	// Layout: border(1) | pad(1) | content(contentW) | pad(1)  →  total = width
	// bubbles input.Width is the text-only area (prompt "> " = 2 chars excluded).
	fieldBg := chrome.fieldBg
	if focused {
		fieldBg = adjustLightness(chrome.baseBg, -0.06)
	}
	contentW := max(1, width-3)

	input.Width = max(1, contentW-2)
	input.PromptStyle = lipgloss.NewStyle().Background(fieldBg).Foreground(chrome.accent).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Background(fieldBg).Foreground(chrome.text)
	placeholderFg := chrome.muted
	if focused {
		placeholderFg = chrome.accent
	}
	input.PlaceholderStyle = lipgloss.NewStyle().Background(fieldBg).Foreground(placeholderFg)
	input.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(chrome.accentFg)
	input.Cursor.TextStyle = lipgloss.NewStyle().Background(chrome.accent).Foreground(chrome.accentFg)

	rendered := ""
	if compactSecretPreview && !focused && input.Value() != "" {
		preview := maskedPreview(input.Value(), 20, chrome.plainUI)
		rendered = lipgloss.NewStyle().Background(fieldBg).Foreground(chrome.muted).Render(preview)
	} else {
		// bubbles pads input.View() with plain un-styled spaces to fill input.Width.
		// Those spaces carry no ANSI bg code, so they show terminal bg.
		// Strip them, then re-pad to contentW with explicit fieldBg-coloured spaces.
		rendered = strings.TrimRight(input.View(), " ")
		rendered = ansi.Truncate(rendered, contentW, "")
	}
	if gap := contentW - lipgloss.Width(rendered); gap > 0 {
		rendered += lipgloss.NewStyle().Background(fieldBg).Render(strings.Repeat(" ", gap))
	}

	// Explicit bg-coloured padding on each side keeps the full row covered.
	pad := lipgloss.NewStyle().Background(fieldBg).Render(" ")
	inner := pad + rendered + pad

	// Thick left bar only (single-line row height). Unfocused uses theme border color so the bar is visible;
	// focused uses accent. (A full box border would add multi-line corners and breaks narrow layouts.)
	barFg := lipgloss.Color(chrome.border)
	if focused {
		barFg = chrome.highlight
	}
	return lipgloss.NewStyle().
		Background(fieldBg).
		BorderLeft(true).
		BorderStyle(lipInputAccentBorder(chrome.plainUI)).
		BorderForeground(barFg).
		BorderBackground(fieldBg).
		Render(inner)
}

func renderSecretSummary(value string, width int, chrome managerChrome) string {
	if width <= 0 {
		return ""
	}

	badgeStyle := lipgloss.NewStyle().
		Background(chrome.surfaceBg).
		Foreground(chrome.muted).
		Width(7).
		Align(lipgloss.Center)

	detailStyle := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted)

	badgeText := "empty"
	detailText := "not set"
	if value != "" {
		badgeText = "saved"
		if chrome.plainUI {
			detailText = fmt.Sprintf("%d chars, id %s", len([]rune(value)), secretFingerprint(value))
		} else {
			detailText = fmt.Sprintf("%d chars • id %s", len([]rune(value)), secretFingerprint(value))
		}
	}

	badge := badgeStyle.Render(badgeText)
	detailW := max(1, width-lipgloss.Width(badge))
	detail := detailStyle.Width(detailW).Render("  " + truncate(detailText, max(1, detailW-2)))
	return badge + detail
}

func renderSecretEditor(input textinput.Model, width int, chrome managerChrome) string {
	fieldBg := chrome.fieldBg
	contentW := max(1, width-4)

	input.Width = max(1, contentW-2)
	input.PromptStyle = lipgloss.NewStyle().Background(fieldBg).Foreground(chrome.accent).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Background(fieldBg).Foreground(chrome.text)
	input.PlaceholderStyle = lipgloss.NewStyle().Background(fieldBg).Foreground(chrome.accent)
	input.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(chrome.accentFg)
	input.Cursor.TextStyle = lipgloss.NewStyle().Background(chrome.accent).Foreground(chrome.accentFg)

	rendered := strings.TrimRight(input.View(), " ")
	rendered = ansi.Truncate(rendered, contentW, "")
	if gap := contentW - lipgloss.Width(rendered); gap > 0 {
		rendered += lipgloss.NewStyle().Background(fieldBg).Render(strings.Repeat(" ", gap))
	}

	inner := lipgloss.NewStyle().
		Background(fieldBg).
		Padding(0, 1).
		Render(rendered)

	return lipgloss.NewStyle().
		Background(fieldBg).
		Border(lipOverlayBorder(chrome.plainUI)).
		BorderForeground(chrome.highlight).
		BorderBackground(fieldBg).
		Render(inner)
}

func maskedPreview(value string, limit int, plain bool) string {
	maskChar := "●"
	if plain {
		maskChar = "*"
	}
	maskCount := len([]rune(value))
	if maskCount == 0 {
		return ""
	}
	if limit < 1 {
		limit = 1
	}
	suffix := "…"
	if plain {
		suffix = "..."
	}
	if maskCount <= limit {
		return strings.Repeat(maskChar, maskCount)
	}
	return strings.Repeat(maskChar, limit) + suffix
}

func secretFingerprint(value string) string {
	if value == "" {
		return ""
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(value))
	return fmt.Sprintf("%06x", h.Sum32())[:6]
}

func renderManagerInput(width int, value, placeholder string, focused bool, chrome managerChrome) string {
	textW := max(1, width-1)
	inputBg := chrome.fieldBg
	dimBg := chrome.surfaceBg
	bg := inputBg
	if !focused {
		bg = dimBg
	}
	cursor := lipgloss.NewStyle().Background(bg).Foreground(chrome.accent).Bold(true)
	text := lipgloss.NewStyle().Background(bg).Foreground(chrome.text)
	ghost := lipgloss.NewStyle().Background(bg).Foreground(chrome.muted)

	value = strings.TrimSpace(value)
	var line string
	if value == "" {
		line = cursor.Render("> ") + ghost.Render(placeholder)
	} else if focused {
		line = cursor.Render("> ") + text.Render(value)
	} else {
		line = text.Render(value)
	}
	return lipgloss.NewStyle().Background(bg).Padding(0, 1).Render(clampView(line, textW, 1, bg))
}

func renderManagerPicker(width int, value string, focused bool, chrome managerChrome, styles Styles) string {
	textW := max(1, width-1)
	bg := chrome.surfaceBg
	fg := chrome.text
	accentFg := chrome.muted
	if focused {
		bg = terminalColorAsColor(styles.FeedItemSelectedFocused.GetBackground())
		fg = terminalColorAsColor(styles.FeedItemSelectedFocused.GetForeground())
		accentFg = fg
	}
	text := lipgloss.NewStyle().Background(bg).Foreground(fg)
	accent := lipgloss.NewStyle().Background(bg).Foreground(accentFg).Bold(true)
	line := accent.Render(chrome.pickerChevronLeft()) + text.Render(truncate(value, max(1, textW-4))) + accent.Render(chrome.pickerChevronRight())
	return lipgloss.NewStyle().Background(bg).Padding(0, 1).Render(clampView(line, textW, 1, bg))
}

func renderManagerColorPicker(width int, option folderColorOption, focused bool, chrome managerChrome, styles Styles) string {
	textW := max(1, width-1)
	bg := chrome.surfaceBg
	fg := chrome.text
	accentFg := chrome.muted
	if focused {
		bg = terminalColorAsColor(styles.FeedItemSelectedFocused.GetBackground())
		fg = terminalColorAsColor(styles.FeedItemSelectedFocused.GetForeground())
		accentFg = fg
	}
	accent := lipgloss.NewStyle().Background(bg).Foreground(accentFg).Bold(true)
	nameStyle := lipgloss.NewStyle().Background(bg).Foreground(fg)
	swatch := lipgloss.NewStyle().
		Background(option.Color).
		Foreground(contrastFg(option.Color)).
		Bold(true).
		Render(" " + strings.ToUpper(option.Name[:min(3, len(option.Name))]) + " ")
	line := accent.Render(chrome.pickerChevronLeft()) + swatch + lipgloss.NewStyle().Background(bg).Render(" ") + nameStyle.Render(option.Name) + accent.Render(chrome.pickerChevronRight())
	return lipgloss.NewStyle().Background(bg).Padding(0, 1).Render(clampView(line, textW, 1, bg))
}

func renderManagerRow(width int, title string, chrome managerChrome) string {
	textW := max(1, width-2)
	rowBg := chrome.baseBg
	row := padRight(truncate(title, textW), textW)
	rendered := lipgloss.NewStyle().
		Background(rowBg).
		Foreground(chrome.text).
		Padding(0, 1).
		Render(row)
	return clampView(rendered, width, 1, rowBg)
}

// managerFolderMarker returns the leading glyph that distinguishes a folder row.
// With icons: nerdfont folder glyphs. Without icons: triangle disclosure (or ASCII on plainUI).
func managerFolderMarker(chrome managerChrome, icons, collapsed bool) string {
	if icons {
		if collapsed {
			return "󰉖 "
		}
		return "󰉋 "
	}
	if chrome.plainUI {
		if collapsed {
			return "> "
		}
		return "v "
	}
	if collapsed {
		return "▸ "
	}
	return "▾ "
}

// managerFeedMarker returns the leading glyph for a feed row.
// Nested rows (feeds that belong to a folder) get a tree connector in front of
// the feed glyph; root-level feeds keep the original flat marker.
// "last" picks the L-shaped connector for the final child of a folder.
func managerFeedMarker(chrome managerChrome, icons, nested, last bool) string {
	tree := ""
	if nested {
		switch {
		case chrome.plainUI && last:
			tree = "`- "
		case chrome.plainUI:
			tree = "|- "
		case last:
			tree = "└─ "
		default:
			tree = "├─ "
		}
	}
	if icons {
		return tree + "\U000f046b "
	}
	if nested {
		return tree
	}
	return "  "
}

func renderManagerFeedRow(width int, title, color string, chrome managerChrome, styles Styles, icons, nested, last bool) string {
	if config.IsRetroTerminalTheme(string(styles.Theme.Name)) {
		color = ""
	}
	textW := max(1, width-2)
	rowBg := chrome.baseBg
	style := lipgloss.NewStyle().
		Background(rowBg).
		Padding(0, 1)
	icon := managerFeedMarker(chrome, icons, nested, last)
	iconW := lipgloss.Width(icon)
	nameW := max(1, textW-iconW)
	name := truncate(title, nameW)
	iconStyle := lipgloss.NewStyle().Foreground(chrome.text).Background(rowBg)
	nameStyle := lipgloss.NewStyle().Foreground(chrome.text).Background(rowBg)
	if color != "" {
		accent := accentReadableOn(lipgloss.Color(color), rowBg, 3)
		iconStyle = iconStyle.Foreground(accent)
		nameStyle = nameStyle.Foreground(accent)
	}
	content := iconStyle.Render(icon) + nameStyle.Render(name)
	fillW := max(0, textW-lipgloss.Width(icon)-lipgloss.Width(name))
	filler := lipgloss.NewStyle().Background(rowBg).Render(strings.Repeat(" ", fillW))
	row := content + filler
	return clampView(style.Render(row), width, 1, rowBg)
}

func renderManagerFolderRow(width int, title, color string, collapsed bool, chrome managerChrome, styles Styles, selected, icons bool) string {
	if config.IsRetroTerminalTheme(string(styles.Theme.Name)) {
		color = ""
	}
	textW := max(1, width-2)
	rowBg := chrome.baseBg
	style := lipgloss.NewStyle().
		Background(rowBg).
		Padding(0, 1)
	if selected {
		style = managerSelectedListStyle(styles)
	}
	contentBg := rowBg
	contentFg := chrome.text
	if selected {
		contentBg = terminalColorAsColor(style.GetBackground())
		contentFg = terminalColorAsColor(style.GetForeground())
	}
	icon := managerFolderMarker(chrome, icons, collapsed)
	iconW := lipgloss.Width(icon)
	nameW := max(1, textW-iconW)
	name := truncate(title, nameW)
	if selected {
		row := padRight(icon+name, textW)
		return clampView(style.Render(row), width, 1, contentBg)
	}
	iconStyle := lipgloss.NewStyle().Foreground(contentFg).Background(contentBg)
	nameStyle := lipgloss.NewStyle().Foreground(contentFg).Background(contentBg)
	if color != "" {
		accent := accentReadableOn(lipgloss.Color(color), contentBg, 3)
		iconStyle = iconStyle.Foreground(accent)
		nameStyle = nameStyle.Foreground(accent)
	}
	content := iconStyle.Render(icon) + nameStyle.Render(name)
	fillW := max(0, textW-lipgloss.Width(icon)-lipgloss.Width(name))
	filler := lipgloss.NewStyle().Background(contentBg).Render(strings.Repeat(" ", fillW))
	row := content + filler
	return clampView(style.Render(row), width, 1, contentBg)
}

func renderManagerSelectedRow(width int, title string, chrome managerChrome, styles Styles) string {
	textW := max(1, width-2)
	row := padRight(truncate(title, textW), textW)
	bg := terminalColorAsColor(managerSelectedListStyle(styles).GetBackground())
	return clampView(managerSelectedListStyle(styles).Render(row), width, 1, bg)
}

func managerSelectedListStyle(styles Styles) lipgloss.Style {
	bg := terminalColorAsColor(styles.FeedItemSelectedFocused.GetBackground())
	if bg != "" {
		if isDark(bg) {
			bg = adjustLightness(bg, 0.08)
		} else {
			bg = adjustLightness(bg, -0.08)
		}
	}
	return lipgloss.NewStyle().
		Background(bg).
		Foreground(readableText(styles.Theme.Fg, bg, 4.5)).
		Bold(true).
		Padding(0, 1)
}

func renderManagerSourceLine(width int, value string, chrome managerChrome) string {
	return lipgloss.NewStyle().
		Width(width).
		Background(chrome.surfaceBg).
		Foreground(chrome.accent).
		Render(padRight(value, width))
}

func renderManagerActionGroups(width int, chrome managerChrome, primaryPairs, secondaryPairs []string) string {
	rows := []string{renderManagerActions(width, chrome, primaryPairs...)}
	if len(secondaryPairs) > 0 {
		rows = append(rows, renderManagerActions(width, chrome, secondaryPairs...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func renderManagerActions(width int, chrome managerChrome, pairs ...string) string {
	bar := lipgloss.NewStyle().
		Width(width).
		Background(chrome.baseBg).
		Border(lipPaneBorder(chrome.plainUI), true, false, false, false).
		BorderForeground(chrome.border).
		Padding(0, 0)
	parts := make([]string, 0, len(pairs)/2)
	spacer := lipgloss.NewStyle().Background(chrome.baseBg).Render(" ")
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, lipgloss.JoinHorizontal(
			lipgloss.Left,
			chrome.key.Render(strings.ToUpper(pairs[i])),
			spacer,
			chrome.keyLabel.Render(strings.ToUpper(pairs[i+1])),
		))
	}
	if len(parts) == 0 {
		return bar.Render(clampView("", width, 1, chrome.baseBg))
	}
	bg := lipgloss.NewStyle().Background(chrome.baseBg)
	sep := bg.Render("  ")
	left := strings.Join(parts[:max(0, len(parts)-1)], sep)
	right := parts[len(parts)-1]
	gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	row := clampView(left+bg.Render(strings.Repeat(" ", gap))+right, width, 1, chrome.baseBg)
	return bar.Render(row)
}

func (fm FeedManager) folderFeedCount(folderID int64) int {
	total := 0
	for _, feed := range fm.feeds {
		if feed.FolderID == folderID {
			total++
		}
	}
	return total
}

func (fm FeedManager) folderUnreadCount(folderID int64) int64 {
	var total int64
	for _, feed := range fm.feeds {
		if feed.FolderID == folderID {
			total += feed.UnreadCount
		}
	}
	return total
}

func (fm FeedManager) confirmDeleteTitle() string {
	if row := fm.selectedRow(); row != nil && row.kind == fmRowFolder {
		return "DELETE FOLDER"
	}
	return "DELETE FEED"
}
