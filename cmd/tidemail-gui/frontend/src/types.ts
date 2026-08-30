export interface Account { ID: number; Name: string; Position: number; Color: string }
export interface Mailbox { ID: number; AccountID: number; Name: string; DisplayName: string; Flags: string[]; UnreadCount: number }
export interface Message {
  ID: number; MailboxID: number; UID: number; Subject: string; From: string; To: string; CC: string;
  ReplyTo: string; Date: string; BodyText: string; Summary: string; Read: boolean; Starred: boolean;
  HasAttachment: boolean; Headers: string; MessageID: string; InReplyTo: string; References: string;
}
export interface Attachment { id: number; filename: string; contentType: string; size: number }
export interface DesktopDisplay { desktopLayout: "native" | "modern"; desktopFolderWidth: number; desktopMessageWidth: number }
export interface Bootstrap { accounts: Account[]; mailboxes: Mailbox[]; config: { theme: string; display: DesktopDisplay; accounts: Array<{ name: string; user: string; from: string }>; aiProvider: string } }
export interface MessagePage { mailboxId: number; query?: string; messages: Message[] }
export interface MessageDetail { message: Message; attachments: Attachment[] }
export interface ComposeRequest {
  accountName: string; to: string[]; cc: string[]; bcc: string[]; subject: string; body: string;
  inReplyTo: string; references: string; attachments: Array<{ name: string; data: string; size?: number }>;
}
export interface AccountSettings {
  name: string; provider: string; imap_host: string; imap_port: number; imap_tls: boolean;
  smtp_host: string; smtp_port: number; smtp_tls: boolean; user: string; password: string;
  from: string; sync_minutes: number; signature: string; refresh_token: string;
}
export interface DesktopSettings { accounts: AccountSettings[] }
