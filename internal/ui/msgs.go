package ui

import (
	"time"

	"golang.org/x/oauth2"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/filter"
	"github.com/allisonhere/tidemail/internal/smtp"
	"github.com/allisonhere/tidemail/internal/update"
)

// DeviceCodeMsg carries the device-code details the user needs to approve a
// Gmail sign-in (visit the URL, enter the code).
type DeviceCodeMsg struct {
	VerificationURL string
	UserCode        string
	Err             error                      // the device-code request itself failed
	da              *oauth2.DeviceAuthResponse // polling handle for the follow-up token cmd
}

// OAuth2DoneMsg reports the outcome of a Gmail sign-in (device-flow polling or
// authorization-code exchange).
type OAuth2DoneMsg struct {
	RefreshToken string
	Err          error
}

type AccountsLoadedMsg struct {
	Accounts  []db.Account
	Mailboxes []db.Mailbox
	Err       error
}

type MessagesLoadedMsg struct {
	MailboxID int64
	Messages  []db.Message
	Search    bool
	Query     string
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

// OlderMessagesLoadedMsg reports the result of paging further back into a
// mailbox's server history. Exhausted means the server holds nothing older, so
// the UI can stop offering to load more.
type OlderMessagesLoadedMsg struct {
	MailboxID int64
	Count     int
	Exhausted bool
	Err       error
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

type MessageStarredUpdatedMsg struct {
	MessageID int64
	MailboxID int64
	Starred   bool
	Err       error
}

type FolderCreatedMsg struct {
	AccountID int64
	MailboxID int64
	Name      string
	Delimiter string
	Err       error
}

type FilterGeneratedMsg struct {
	English string
	Rule    filter.Rule
	Err     error
}

type FilterFoldersPreparedMsg struct {
	Intent    filterSaveIntent
	Mailboxes []db.Mailbox
	Err       error
}

type FilterRunMsg struct {
	Matched int
	Applied int
	DryRun  bool
	Err     error
}

// MessagesDeletedMsg reports the outcome of a (possibly bulk) delete.
// Deleted lists messages removed both remotely and locally; messages whose
// remote delete failed are left untouched (still in the DB and list) so the
// local state never silently diverges from the server.
type MessagesDeletedMsg struct {
	Deleted []MessageRef
	Failed  int
	Err     error // first remote/local error, when Failed > 0
}

type MessageRef struct {
	ID        int64
	MailboxID int64
}

type ClipboardReadMsg struct {
	Text string
	Err  error
}

type MailboxReadUpdatedMsg struct {
	MailboxIDs []int64
	Err        error
}

type MessageSentMsg struct {
	Err       error
	DraftID   int64  // draft to delete on success (0 = none)
	PendingID uint64 // pending-send entry to clear (0 = immediate send)
}

// SendQueuedMsg is emitted by the compose overlay when the user hits send;
// the model decides whether to dispatch immediately or hold the message for
// the configured undo grace period.
type SendQueuedMsg struct {
	Account config.AccountConfig
	Msg     smtp.OutgoingMessage
}

// CommitSendMsg fires when a pending send's grace period elapses.
type CommitSendMsg struct {
	ID uint64
}

type DraftSavedMsg struct {
	DraftID int64
	Err     error
}

type DraftDeletedMsg struct {
	DraftID int64
	Err     error
}

type DraftsLoadedMsg struct {
	MailboxID int64
	Drafts    []db.Draft
	Err       error
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

type UpdateProgressTickMsg struct{}

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

type AutoSyncMsg struct {
	AccountID int64
}

// MailboxesRefreshedMsg reports the result of an account-level folder LIST.
// Mailboxes carries folders newly discovered server-side (additions) and
// Removed carries the IDs of locally pruned folders that vanished server-side.
// Err is non-nil when the LIST failed, in which case it's surfaced quietly
// since folder refresh is a convenience, not part of mail delivery.
type MailboxesRefreshedMsg struct {
	AccountID int64
	Mailboxes []db.Mailbox
	Removed   []int64
	Err       error
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
