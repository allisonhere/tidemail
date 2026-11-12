package ui

import tea "github.com/charmbracelet/bubbletea"

// selectedAddressView returns the local recipient view under the current account.
func (m *Model) selectedAddressView() *sidebarRow {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return nil
	}
	row := m.sidebarRows[m.sidebarCursor]
	if row.kind != rowKindAddressView {
		return nil
	}
	return &row
}

func (m *Model) loadAddressViewCmd() tea.Cmd {
	row := m.selectedAddressView()
	if row == nil {
		return nil
	}
	database, version, unread := m.db, m.readVersion, m.showUnreadOnly
	return func() tea.Msg {
		if database == nil {
			return MessagesLoadedMsg{ReadVersion: version, AddressView: row.label, AddressAccountID: row.accountID}
		}
		messages, err := database.ListAddressInbox(row.accountID, row.label, unread)
		if err != nil {
			return MessagesLoadedMsg{ReadVersion: version, AddressView: row.label, AddressAccountID: row.accountID, Err: err}
		}
		annotations, overrides := loadMessageClassification(database, messages)
		return MessagesLoadedMsg{ReadVersion: version, AddressView: row.label, AddressAccountID: row.accountID, Messages: messages, Annotations: annotations, Overrides: overrides}
	}
}

func (m *Model) syncAddressViewCmd() tea.Cmd {
	view := m.selectedAddressView()
	if view == nil {
		return nil
	}
	for _, mailbox := range m.mailboxes {
		if mailbox.AccountID == view.accountID && isInboxMailbox(mailbox) {
			return m.syncMailboxCmd(mailbox.ID, true)
		}
	}
	return nil
}
