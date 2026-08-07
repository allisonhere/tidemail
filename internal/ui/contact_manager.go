package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type cmMode int

const (
	cmList cmMode = iota
	cmAdd
	cmEdit
	cmConfirmDelete
	cmPickSeen
	cmPickFile
)

type ContactManager struct {
	db *db.DB

	contacts []db.Contact
	cursor   int
	marked   map[int]bool
	mode     cmMode

	nameInput         textinput.Model
	emailInput        textinput.Model
	phoneInput        textinput.Model
	organizationInput textinput.Model
	titleInput        textinput.Model
	noteInput         textinput.Model
	focusEmail        bool
	formFocus         int
	editID            int64

	seen       []db.Contact
	filter     textinput.Model
	pickCursor int
	pickMarked map[int]bool

	filePicker filePicker
	importing  bool
	composeTo  []string
	statusMsg  string
	isErr      bool
}

func NewContactManager(database *db.DB) ContactManager {
	cm := ContactManager{
		db:         database,
		marked:     map[int]bool{},
		pickMarked: map[int]bool{},
	}
	cm.nameInput = newAMInput("Display name (optional)", false)
	cm.emailInput = newAMInput("you@example.com", false)
	cm.phoneInput = newAMInput("phone (optional)", false)
	cm.organizationInput = newAMInput("organization (optional)", false)
	cm.titleInput = newAMInput("title (optional)", false)
	cm.noteInput = newAMInput("note (optional)", false)
	cm.filter = newAMInput("filter seen mail", false)
	cm.reload()
	return cm
}

func (cm *ContactManager) reload() {
	cm.contacts, _ = cm.db.ListContacts()
	cm.clampCursor()
}

func (cm *ContactManager) clampCursor() {
	if cm.cursor < 0 {
		cm.cursor = 0
	}
	if len(cm.contacts) == 0 {
		cm.cursor = 0
		return
	}
	if cm.cursor >= len(cm.contacts) {
		cm.cursor = len(cm.contacts) - 1
	}
}

func (cm *ContactManager) selected() (db.Contact, bool) {
	if cm.cursor < 0 || cm.cursor >= len(cm.contacts) {
		return db.Contact{}, false
	}
	return cm.contacts[cm.cursor], true
}

func (cm *ContactManager) setStatus(msg string, isErr bool) {
	cm.statusMsg = msg
	cm.isErr = isErr
}

func (cm *ContactManager) clearMarks() {
	cm.marked = map[int]bool{}
}

func (cm ContactManager) targetIndexes() []int {
	var indexes []int
	for i := range cm.contacts {
		if cm.marked[i] {
			indexes = append(indexes, i)
		}
	}
	if len(indexes) > 0 {
		return indexes
	}
	if _, ok := cm.selected(); ok {
		return []int{cm.cursor}
	}
	return nil
}

func (cm ContactManager) targetContacts() []db.Contact {
	indexes := cm.targetIndexes()
	contacts := make([]db.Contact, 0, len(indexes))
	for _, i := range indexes {
		if i >= 0 && i < len(cm.contacts) {
			contacts = append(contacts, cm.contacts[i])
		}
	}
	return contacts
}

func contactAddress(c db.Contact) string {
	if c.DisplayName != "" {
		return c.DisplayName + " <" + c.Addr + ">"
	}
	return c.Addr
}

func contactAddressList(contacts []db.Contact) []string {
	addrs := make([]string, 0, len(contacts))
	for _, c := range contacts {
		addrs = append(addrs, contactAddress(c))
	}
	return addrs
}

func (cm *ContactManager) toggleMarkAdvance() {
	if len(cm.contacts) == 0 {
		return
	}
	if cm.marked[cm.cursor] {
		delete(cm.marked, cm.cursor)
	} else {
		cm.marked[cm.cursor] = true
	}
	if cm.cursor < len(cm.contacts)-1 {
		cm.cursor++
	}
	if len(cm.marked) > 0 {
		cm.setStatus(fmt.Sprintf("%d selected", len(cm.marked)), false)
	} else {
		cm.statusMsg = ""
	}
}

func (cm ContactManager) Update(msg tea.Msg, keys KeyMap) (ContactManager, tea.Cmd, bool) {
	switch cm.mode {
	case cmAdd, cmEdit:
		return cm.updateForm(msg, keys)
	case cmConfirmDelete:
		return cm.updateConfirmDelete(msg, keys)
	case cmPickSeen:
		return cm.updatePicker(msg, keys)
	case cmPickFile:
		return cm.updateFilePicker(msg, keys)
	default:
		return cm.updateList(msg, keys)
	}
}

func (cm ContactManager) updateList(msg tea.Msg, keys KeyMap) (ContactManager, tea.Cmd, bool) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return cm, nil, false
	}
	switch {
	case keyMatches(km, keys.Cancel), keyMatches(km, keys.Back):
		return cm, nil, true
	case keyMatches(km, keys.Up):
		if cm.cursor > 0 {
			cm.cursor--
		}
	case keyMatches(km, keys.Down):
		if cm.cursor < len(cm.contacts)-1 {
			cm.cursor++
		}
	case keyMatches(km, keys.Space):
		cm.toggleMarkAdvance()
	case km.String() == "c":
		contacts := cm.targetContacts()
		if len(contacts) == 0 {
			cm.setStatus("no contacts to compose", true)
			return cm, nil, false
		}
		cm.composeTo = contactAddressList(contacts)
	case km.String() == "n":
		cm.beginAdd()
	case keyMatches(km, keys.Edit):
		cm.beginEdit()
	case keyMatches(km, keys.Delete):
		if len(cm.targetIndexes()) > 0 {
			cm.mode = cmConfirmDelete
		}
	case km.String() == "f":
		cm.beginPicker()
	case km.String() == "i":
		cm.beginFilePicker(true)
	case km.String() == "x":
		cm.beginFilePicker(false)
	}
	return cm, nil, false
}

func (cm *ContactManager) beginAdd() {
	cm.mode = cmAdd
	cm.editID = 0
	cm.nameInput.Reset()
	cm.emailInput.Reset()
	cm.phoneInput.Reset()
	cm.organizationInput.Reset()
	cm.titleInput.Reset()
	cm.noteInput.Reset()
	cm.setFormFocus(0)
	cm.statusMsg = ""
}

func (cm *ContactManager) beginEdit() {
	c, ok := cm.selected()
	if !ok {
		return
	}
	cm.mode = cmEdit
	cm.editID = c.ID
	cm.nameInput.SetValue(c.DisplayName)
	cm.emailInput.SetValue(c.Addr)
	cm.phoneInput.SetValue(c.Phone)
	cm.organizationInput.SetValue(c.Organization)
	cm.titleInput.SetValue(c.Title)
	cm.noteInput.SetValue(c.Note)
	cm.setFormFocus(0)
	cm.statusMsg = ""
}

func (cm *ContactManager) beginPicker() {
	cm.mode = cmPickSeen
	cm.seen, _ = cm.db.SeenAddresses()
	cm.filter.Reset()
	cm.filter.Focus()
	cm.pickCursor = 0
	cm.pickMarked = map[int]bool{}
	cm.statusMsg = ""
}

func (cm *ContactManager) contactFormInputs() []*textinput.Model {
	return []*textinput.Model{
		&cm.nameInput,
		&cm.emailInput,
		&cm.phoneInput,
		&cm.organizationInput,
		&cm.titleInput,
		&cm.noteInput,
	}
}

func (cm *ContactManager) setFormFocus(index int) {
	inputs := cm.contactFormInputs()
	cm.formFocus = clamp(index, 0, len(inputs)-1)
	for i, input := range inputs {
		if i == cm.formFocus {
			input.Focus()
		} else {
			input.Blur()
		}
	}
	cm.focusEmail = cm.formFocus == 1
}

func (cm *ContactManager) beginFilePicker(importing bool) {
	cm.mode = cmPickFile
	cm.importing = importing
	cm.statusMsg = ""
	dir, err := os.UserHomeDir()
	if err != nil {
		cm.setStatus("home dir: "+err.Error(), true)
		return
	}
	if !importing {
		if downloads := filepath.Join(dir, "Downloads"); dirExists(downloads) {
			dir = downloads
		}
	}
	cm.openContactFilePicker(dir)
}

func (cm ContactManager) updateForm(msg tea.Msg, keys KeyMap) (ContactManager, tea.Cmd, bool) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		inputs := cm.contactFormInputs()
		if cm.formFocus >= 0 && cm.formFocus < len(inputs) {
			*inputs[cm.formFocus], cmd = inputs[cm.formFocus].Update(msg)
		}
		return cm, cmd, false
	}
	switch {
	case keyMatches(km, keys.Cancel), keyMatches(km, keys.Back):
		cm.mode = cmList
		cm.statusMsg = ""
		return cm, nil, false
	case keyMatches(km, keys.Tab), keyMatches(km, keys.Up), keyMatches(km, keys.Down):
		cm.setFormFocus((cm.formFocus + 1) % len(cm.contactFormInputs()))
		return cm, nil, false
	case keyMatches(km, keys.Enter):
		return cm.submitForm(), nil, false
	}
	var cmd tea.Cmd
	inputs := cm.contactFormInputs()
	if cm.formFocus >= 0 && cm.formFocus < len(inputs) {
		*inputs[cm.formFocus], cmd = inputs[cm.formFocus].Update(msg)
	}
	return cm, cmd, false
}

func (cm ContactManager) submitForm() ContactManager {
	addr := strings.TrimSpace(cm.emailInput.Value())
	name := strings.TrimSpace(cm.nameInput.Value())
	meta := db.ContactMetadata{
		Phone:        cm.phoneInput.Value(),
		Organization: cm.organizationInput.Value(),
		Title:        cm.titleInput.Value(),
		Note:         cm.noteInput.Value(),
	}
	if addr == "" {
		cm.setStatus("address is required", true)
		return cm
	}
	if cm.mode == cmEdit && cm.editID != 0 {
		if err := cm.db.UpdateContactWithMetadata(cm.editID, addr, name, meta); err != nil {
			cm.setStatus("save failed: "+err.Error(), true)
			return cm
		}
		cm.setStatus("updated "+addr, false)
	} else {
		if _, err := cm.db.AddContactWithMetadata(addr, name, "manual", meta); err != nil {
			cm.setStatus("save failed: "+err.Error(), true)
			return cm
		}
		cm.setStatus("added "+addr, false)
	}
	cm.mode = cmList
	cm.reload()
	return cm
}

func (cm ContactManager) updateConfirmDelete(msg tea.Msg, keys KeyMap) (ContactManager, tea.Cmd, bool) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return cm, nil, false
	}
	switch {
	case keyMatches(km, keys.Yes):
		indexes := cm.targetIndexes()
		for _, i := range indexes {
			if i >= 0 && i < len(cm.contacts) {
				if err := cm.db.DeleteContact(cm.contacts[i].ID); err != nil {
					cm.setStatus("delete failed: "+err.Error(), true)
					break
				}
			}
		}
		if cm.statusMsg == "" || !cm.isErr {
			cm.setStatus(fmt.Sprintf("deleted %d contact(s)", len(indexes)), false)
		}
		cm.clearMarks()
		cm.mode = cmList
		cm.reload()
	case keyMatches(km, keys.No), keyMatches(km, keys.Cancel), keyMatches(km, keys.Back):
		cm.mode = cmList
	}
	return cm, nil, false
}

func (cm ContactManager) filteredSeen() []db.Contact {
	q := strings.ToLower(strings.TrimSpace(cm.filter.Value()))
	if q == "" {
		return cm.seen
	}
	out := make([]db.Contact, 0, len(cm.seen))
	for _, c := range cm.seen {
		haystack := strings.ToLower(c.DisplayName + " " + c.Addr)
		if strings.Contains(haystack, q) {
			out = append(out, c)
		}
	}
	return out
}

func (cm *ContactManager) clampPickCursor(filtered []db.Contact) {
	if cm.pickCursor < 0 {
		cm.pickCursor = 0
	}
	if len(filtered) == 0 {
		cm.pickCursor = 0
		return
	}
	if cm.pickCursor >= len(filtered) {
		cm.pickCursor = len(filtered) - 1
	}
}

func (cm *ContactManager) togglePickMarkAdvance(filtered []db.Contact) {
	if len(filtered) == 0 {
		return
	}
	c := filtered[cm.pickCursor]
	idx := cm.seenIndex(c)
	if idx < 0 {
		return
	}
	if cm.pickMarked[idx] {
		delete(cm.pickMarked, idx)
	} else {
		cm.pickMarked[idx] = true
	}
	if cm.pickCursor < len(filtered)-1 {
		cm.pickCursor++
	}
	if len(cm.pickMarked) > 0 {
		cm.setStatus(fmt.Sprintf("%d selected", len(cm.pickMarked)), false)
	} else {
		cm.statusMsg = ""
	}
}

func (cm ContactManager) seenIndex(contact db.Contact) int {
	for i, c := range cm.seen {
		if c.Addr == contact.Addr {
			return i
		}
	}
	return -1
}

func (cm ContactManager) updatePicker(msg tea.Msg, keys KeyMap) (ContactManager, tea.Cmd, bool) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		var cmd tea.Cmd
		cm.filter, cmd = cm.filter.Update(msg)
		cm.clampPickCursor(cm.filteredSeen())
		return cm, cmd, false
	}
	switch {
	case keyMatches(km, keys.Cancel), keyMatches(km, keys.Back):
		cm.mode = cmList
		cm.statusMsg = ""
		return cm, nil, false
	case keyMatches(km, keys.Up):
		if cm.pickCursor > 0 {
			cm.pickCursor--
		}
		return cm, nil, false
	case keyMatches(km, keys.Down):
		filtered := cm.filteredSeen()
		if cm.pickCursor < len(filtered)-1 {
			cm.pickCursor++
		}
		return cm, nil, false
	case keyMatches(km, keys.Space):
		cm.togglePickMarkAdvance(cm.filteredSeen())
		return cm, nil, false
	case keyMatches(km, keys.Enter), km.String() == "a":
		return cm.addPickedContacts(), nil, false
	}
	var cmd tea.Cmd
	cm.filter, cmd = cm.filter.Update(msg)
	cm.clampPickCursor(cm.filteredSeen())
	return cm, cmd, false
}

func (cm ContactManager) addPickedContacts() ContactManager {
	filtered := cm.filteredSeen()
	var picks []db.Contact
	for idx := range cm.pickMarked {
		if idx >= 0 && idx < len(cm.seen) {
			picks = append(picks, cm.seen[idx])
		}
	}
	if len(picks) == 0 && len(filtered) > 0 {
		picks = []db.Contact{filtered[cm.pickCursor]}
	}
	for _, c := range picks {
		if _, err := cm.db.AddContact(c.Addr, c.DisplayName, "manual"); err != nil {
			cm.setStatus("add failed: "+err.Error(), true)
			return cm
		}
	}
	cm.mode = cmList
	cm.pickMarked = map[int]bool{}
	cm.reload()
	cm.setStatus(fmt.Sprintf("added %d contact(s)", len(picks)), false)
	return cm
}

func (cm *ContactManager) openContactFilePicker(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		cm.setStatus("pick: "+err.Error(), true)
		cm.filePicker.active = false
		return
	}

	var fe []fileEntry
	if !cm.importing {
		fe = append(fe, fileEntry{name: "✓ select this folder", isDir: false, size: 0})
	}
	if dir != "/" {
		fe = append(fe, fileEntry{name: "..", isDir: true})
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		fe = append(fe, fileEntry{name: entry.Name(), isDir: entry.IsDir(), size: info.Size()})
	}
	sort.Slice(fe, func(i, j int) bool {
		if fe[i].name == "✓ select this folder" {
			return true
		}
		if fe[j].name == "✓ select this folder" {
			return false
		}
		if fe[i].isDir != fe[j].isDir {
			return fe[i].isDir
		}
		return strings.ToLower(fe[i].name) < strings.ToLower(fe[j].name)
	})
	cm.filePicker.currentDir = dir
	cm.filePicker.entries = fe
	cm.filePicker.cursor = 0
	cm.filePicker.active = true
}

func (cm ContactManager) updateFilePicker(msg tea.Msg, keys KeyMap) (ContactManager, tea.Cmd, bool) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return cm, nil, false
	}
	switch {
	case keyMatches(km, keys.Cancel), keyMatches(km, keys.Back):
		cm.mode = cmList
		cm.statusMsg = ""
		return cm, nil, false
	case keyMatches(km, keys.Up):
		if cm.filePicker.cursor > 0 {
			cm.filePicker.cursor--
		}
		return cm, nil, false
	case keyMatches(km, keys.Down):
		if cm.filePicker.cursor < len(cm.filePicker.entries)-1 {
			cm.filePicker.cursor++
		}
		return cm, nil, false
	case keyMatches(km, keys.Left):
		cm.openContactFilePicker(filepath.Dir(cm.filePicker.currentDir))
		return cm, nil, false
	case keyMatches(km, keys.Enter):
		return cm.confirmFilePicker(), nil, false
	}
	if len(km.String()) == 1 && ((km.String() >= "a" && km.String() <= "z") || (km.String() >= "A" && km.String() <= "Z")) {
		lower := strings.ToLower(km.String())
		for i, entry := range cm.filePicker.entries {
			if strings.HasPrefix(strings.ToLower(entry.name), lower) {
				cm.filePicker.cursor = i
				break
			}
		}
	}
	return cm, nil, false
}

func (cm ContactManager) confirmFilePicker() ContactManager {
	if cm.filePicker.cursor < 0 || cm.filePicker.cursor >= len(cm.filePicker.entries) {
		return cm
	}
	entry := cm.filePicker.entries[cm.filePicker.cursor]
	if entry.isDir {
		if entry.name == ".." {
			cm.openContactFilePicker(filepath.Dir(cm.filePicker.currentDir))
		} else {
			cm.openContactFilePicker(filepath.Join(cm.filePicker.currentDir, entry.name))
		}
		return cm
	}
	if cm.importing {
		path := filepath.Join(cm.filePicker.currentDir, entry.name)
		f, err := os.Open(path)
		if err != nil {
			cm.setStatus("open failed: "+err.Error(), true)
			return cm
		}
		defer f.Close()
		n, err := cm.db.ImportVCard(f)
		if err != nil {
			cm.setStatus("import failed: "+err.Error(), true)
			return cm
		}
		cm.mode = cmList
		cm.reload()
		cm.setStatus(fmt.Sprintf("imported %d contacts", n), false)
		return cm
	}
	path := filepath.Join(cm.filePicker.currentDir, "contacts.vcf")
	f, err := os.Create(path)
	if err != nil {
		cm.setStatus("create failed: "+err.Error(), true)
		return cm
	}
	defer f.Close()
	if err := cm.db.ExportVCard(f); err != nil {
		cm.setStatus("export failed: "+err.Error(), true)
		return cm
	}
	cm.mode = cmList
	cm.setStatus(fmt.Sprintf("exported %d contacts to %s", len(cm.contacts), path), false)
	return cm
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (cm ContactManager) View(width, height int, styles Styles) string {
	chrome := newManagerChrome(width, styles.Theme, styles.PlainUI)
	switch cm.mode {
	case cmAdd:
		return cm.viewForm(width, height, chrome)
	case cmEdit:
		return cm.viewForm(width, height, chrome)
	case cmConfirmDelete:
		return cm.viewConfirmDelete(width, chrome)
	case cmPickSeen:
		return cm.viewPicker(width, height, chrome, styles)
	case cmPickFile:
		return cm.viewFilePicker(width, height, chrome)
	default:
		return cm.viewList(width, height, chrome, styles)
	}
}

// softTitle is the lowercase mode name shown in the overlay's rounded border,
// mirroring accountManager.softTitle.
func (cm ContactManager) softTitle() string {
	switch cm.mode {
	case cmAdd:
		return "add contact"
	case cmEdit:
		return "edit contact"
	case cmConfirmDelete:
		return "delete contact?"
	case cmPickSeen:
		return "add from mail"
	case cmPickFile:
		if cm.importing {
			return "import contacts"
		}
		return "export contacts"
	default:
		return "contacts"
	}
}

func (cm ContactManager) viewList(width, height int, chrome managerChrome, styles Styles) string {
	lines := make([]string, 0, max(1, len(cm.contacts)))
	anchor := 0
	if len(cm.contacts) == 0 {
		lines = append(lines, lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(width).Padding(0, 2).Render("No contacts."))
	} else {
		for i, c := range cm.contacts {
			selected := i == cm.cursor
			if selected {
				anchor = i
			}
			lines = append(lines, cm.renderContactRow(width, chrome, styles, c, i, selected))
		}
	}
	bodyH := max(1, height-4)
	offset := settingsScrollOffset(len(lines), anchor, bodyH)
	end := min(len(lines), offset+bodyH)
	body := clampView(
		lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(strings.Join(lines[offset:end], "\n")),
		width, bodyH, chrome.baseBg,
	)
	parts := []string{body}
	if status := cm.statusLine(width, chrome); status != "" {
		parts = append(parts, status)
	}
	parts = append(parts, lipgloss.JoinVertical(lipgloss.Left,
		renderSoftHints(width, chrome, "space", "select", "c", "compose", "n", "new", "f", "from mail", "i", "import", "x", "export"),
		renderSoftHints(width, chrome, "e", "edit", "d", "delete", "esc", "close"),
	))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (cm ContactManager) renderContactRow(width int, chrome managerChrome, styles Styles, c db.Contact, index int, selected bool) string {
	label := c.Addr
	if c.DisplayName != "" {
		label = fmt.Sprintf("%s  <%s>", c.DisplayName, c.Addr)
	}
	mark := "  "
	if cm.marked[index] {
		if chrome.plainUI {
			mark = "* "
		} else {
			mark = "✓ "
		}
	}
	labelW := max(1, width-2) // minus the 2-cell rail
	cell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Render(" " + truncate(mark+label, max(1, labelW-1)))
	return softRail(chrome, selected, chrome.baseBg) + padStyled(cell, labelW, chrome.baseBg)
}

func (cm ContactManager) statusLine(width int, chrome managerChrome) string {
	if cm.statusMsg == "" {
		return ""
	}
	fg := chrome.successFg
	if cm.isErr {
		fg = chrome.errorFg
	}
	return lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Width(width).Padding(0, 1).Render(cm.statusMsg)
}

func (cm ContactManager) viewForm(width, height int, chrome managerChrome) string {
	labelW := clamp(width/5, 8, 14)
	field := func(lbl string, in textinput.Model, focused bool) string {
		rowFieldW := max(1, width-2-labelW)
		control := renderInsetControl(renderTextInput(in, max(1, rowFieldW-4), focused, false, chrome), rowFieldW, 2, chrome)
		return renderSoftRow(lbl, focused, control, width, labelW, chrome)
	}
	blank := lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")
	rows := []string{
		field("Name", cm.nameInput, cm.formFocus == 0),
		blank,
		field("Email", cm.emailInput, cm.formFocus == 1),
		blank,
		field("Phone", cm.phoneInput, cm.formFocus == 2),
		blank,
		field("Org", cm.organizationInput, cm.formFocus == 3),
		blank,
		field("Title", cm.titleInput, cm.formFocus == 4),
		blank,
		field("Note", cm.noteInput, cm.formFocus == 5),
	}
	bodyH := max(1, height-5)
	anchor := clamp(cm.formFocus*2, 0, max(0, len(rows)-1))
	offset := settingsScrollOffset(len(rows), anchor, bodyH)
	end := min(len(rows), offset+bodyH)
	body := clampView(lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(strings.Join(rows[offset:end], "\n")), width, bodyH, chrome.baseBg)
	parts := []string{body}
	if status := cm.statusLine(width, chrome); status != "" {
		parts = append(parts, status)
	}
	parts = append(parts, renderSoftHints(width, chrome, "enter", "save", "tab", "next field", "esc", "cancel"))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (cm ContactManager) viewConfirmDelete(width int, chrome managerChrome) string {
	prompt := "Delete this contact?"
	if ids := cm.targetIndexes(); len(ids) > 1 {
		prompt = fmt.Sprintf("Delete %d selected contacts?", len(ids))
	} else if c, ok := cm.selected(); ok {
		prompt = fmt.Sprintf("Delete %q from your contacts?", c.Addr)
	}
	body := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Width(width).Padding(1, 2).Render(prompt)
	hints := renderSoftHints(width, chrome, "y", "delete", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, body, hints)
}

func (cm ContactManager) viewPicker(width, height int, chrome managerChrome, styles Styles) string {
	filterW := max(8, width-2)
	filter := cm.filter
	filter.PromptStyle = lipgloss.NewStyle().Background(chrome.fieldBg).Foreground(chrome.accent)
	filter.TextStyle = lipgloss.NewStyle().Background(chrome.fieldBg).Foreground(chrome.text)
	filter.PlaceholderStyle = lipgloss.NewStyle().Background(chrome.fieldBg).Foreground(chrome.muted)
	if filter.Value() == "" {
		// bubbles' placeholderView pads to Width with unstyled spaces when Width
		// is set, leaking the terminal's default background. Leave padding to
		// the Width(filterW) wrap below, which styles it.
		filter.Width = 0
	}
	filterLine := lipgloss.NewStyle().Background(chrome.fieldBg).Foreground(chrome.text).Width(filterW).Padding(0, 1).
		Render(truncateStyled(inputViewWithCursor(filter, true), max(1, filterW-2), chrome.fieldBg))
	filtered := cm.filteredSeen()
	anchor := cm.pickCursor
	lines := make([]string, 0, max(1, len(filtered)))
	if len(filtered) == 0 {
		lines = append(lines, lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(width).Padding(0, 1).Render("No matching addresses."))
	} else {
		for i, c := range filtered {
			lines = append(lines, cm.renderPickerRow(width, chrome, styles, c, i == cm.pickCursor))
		}
	}
	bodyH := max(1, height-lipgloss.Height(filterLine)-5)
	offset := settingsScrollOffset(len(lines), anchor, bodyH)
	end := min(len(lines), offset+bodyH)
	body := clampView(lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(strings.Join(lines[offset:end], "\n")), width, bodyH, chrome.baseBg)
	parts := []string{filterLine, body}
	if status := cm.statusLine(width, chrome); status != "" {
		parts = append(parts, status)
	}
	parts = append(parts, renderSoftHints(width, chrome, "space", "select", "enter", "add", "esc", "cancel"))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (cm ContactManager) renderPickerRow(width int, chrome managerChrome, styles Styles, c db.Contact, selected bool) string {
	label := c.Addr
	if c.DisplayName != "" {
		label = fmt.Sprintf("%s  <%s>", c.DisplayName, c.Addr)
	}
	mark := "  "
	if idx := cm.seenIndex(c); idx >= 0 && cm.pickMarked[idx] {
		if chrome.plainUI {
			mark = "* "
		} else {
			mark = "✓ "
		}
	}
	labelW := max(1, width-2) // minus the 2-cell rail
	cell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Render(" " + truncate(mark+label, max(1, labelW-1)))
	return softRail(chrome, selected, chrome.baseBg) + padStyled(cell, labelW, chrome.baseBg)
}

func (cm ContactManager) viewFilePicker(width, height int, chrome managerChrome) string {
	dirLine := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted).
		Width(width).
		Padding(0, 2).
		Render(clampView(cm.filePicker.currentDir, width-2, 1, chrome.baseBg))

	labelW := max(1, width-2) // minus the 2-cell rail
	listH := max(1, height-lipgloss.Height(dirLine)-4)
	start := 0
	if cm.filePicker.cursor >= listH {
		start = cm.filePicker.cursor - listH + 1
	}
	end := min(start+listH, len(cm.filePicker.entries))
	visible := cm.filePicker.entries[start:end]

	var rows []string
	for i, entry := range visible {
		idx := start + i
		selected := idx == cm.filePicker.cursor
		label := contactFileEntryLabel(entry)
		// Selection is the accent rail; entries keep their semantic colour.
		fg := chrome.text
		switch {
		case selected:
			fg = chrome.text
		case entry.isDir:
			fg = chrome.accent
		case entry.name == "✓ select this folder":
			fg = chrome.successFg
		}
		cell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Render(" " + truncate(label, max(1, labelW-1)))
		rows = append(rows, softRail(chrome, selected, chrome.baseBg)+padStyled(cell, labelW, chrome.baseBg))
	}
	for len(rows) < listH {
		rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(""))
	}
	body := lipgloss.JoinVertical(lipgloss.Left, rows...)

	parts := []string{dirLine, body}
	if status := cm.statusLine(width, chrome); status != "" {
		parts = append(parts, status)
	}
	action := "open/import"
	if !cm.importing {
		action = "open/export"
	}
	parts = append(parts, renderSoftHints(width, chrome, "enter", action, "←", "parent", "esc", "cancel"))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func contactFileEntryLabel(entry fileEntry) string {
	switch {
	case entry.name == "✓ select this folder":
		return entry.name
	case entry.isDir && entry.name == "..":
		return "📁 ../"
	case entry.isDir:
		return "📁 " + entry.name
	default:
		return "📄 " + entry.name
	}
}
