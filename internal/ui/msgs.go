package ui

import (
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/update"
)

type AccountsLoadedMsg struct {
	Accounts  []db.Account
	Mailboxes []db.Mailbox
	Err       error
}

type MessagesLoadedMsg struct {
	MailboxID int64
	Messages  []db.Message
	Err       error
}

type MailboxSyncedMsg struct {
	MailboxID   int64
	NewCount    int
	NewMessages []db.Message // genuinely-new unread mail, for notification sender/subject
	Err         error
	Manual      bool
	Total       time.Duration
}

type AccountSavedMsg struct {
	Account    db.Account
	Mailboxes  []db.Mailbox
	AccountCfg config.AccountConfig
	Err        error
}

type AccountTestedMsg struct {
	MailboxCount int
	Err          error
}

type AccountDeletedMsg struct {
	AccountID int64
	Err       error
}

type MessageReadUpdatedMsg struct {
	MessageID int64
	MailboxID int64
	WasRead   bool
	Read      bool
	Advance   bool
	Err       error
}

type MessageMovedMsg struct {
	MessageID     int64
	FromMailboxID int64
	ToMailboxID   int64
	Action        string
	Err           error
}

type MessageDeletedMsg struct {
	MessageID    int64
	MailboxID    int64
	LocalDeleted bool
	Err          error
}

type MailboxReadUpdatedMsg struct {
	MailboxIDs []int64
	Err        error
}

type MessageSentMsg struct {
	Err error
}

type AISummaryFetchedMsg struct {
	MessageID int64
	Summary   string
	Err       error
}

// AIValidateDoneMsg is sent after settings triggers an async AI credential check.
type AIValidateDoneMsg struct {
	Err error
}

type UpdateCheckedMsg struct {
	Result update.CheckResult
	Manual bool
	Err    error
}

type UpdateDownloadedMsg struct {
	Asset update.DownloadedAsset
	Err   error
}

type UpdateInstalledMsg struct {
	Result update.InstallResult
	Err    error
}

type RestartedMsg struct {
	Err error
}

type SummarySavedMsg struct {
	Path string
	Err  error
}

type ClipboardCopiedMsg struct {
	Err error
}

type AttachmentsSavedMsg struct {
	Path  string
	Count int
	Err   error
}

type StatusClearMsg struct{}

type OAuth2DoneMsg struct {
	ClientID     string
	ClientSecret string
	AccessToken  string
	RefreshToken string
	Err          error
}

type AutoSyncMsg struct {
	AccountID int64
}

type AddressBookLoadedMsg struct {
	Addresses []string
	Err       error
}

type ErrMsg struct{ Err error }

type GrammarCheckedMsg struct {
	Corrected string
	Err       error
}
