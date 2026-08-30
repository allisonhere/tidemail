import type { Bootstrap, ComposeRequest, DesktopSettings, Message, MessageDetail, MessagePage } from "./types";

interface DesktopAPI {
  Bootstrap(): Promise<Bootstrap>;
  ListMessages(mailboxId: number, unified: boolean, unreadOnly: boolean): Promise<MessagePage>;
  Search(query: string): Promise<MessagePage>;
  SyncMailbox(mailboxId: number): Promise<{ mailboxId: number; fetched: number; new: number }>;
  Message(id: number): Promise<MessageDetail>;
  SetRead(id: number, read: boolean): Promise<void>;
  SetStarred(id: number, starred: boolean): Promise<void>;
  Archive(id: number): Promise<void>;
  Delete(id: number): Promise<void>;
  PickAttachments(): Promise<Array<{ name: string; data: string; size: number }>>;
  SaveAttachment(messageId: number, attachmentId: number): Promise<string>;
  Send(request: ComposeRequest): Promise<void>;
  Summarize(id: number): Promise<string>;
  Settings(): Promise<DesktopSettings>;
  SaveSettings(settings: DesktopSettings): Promise<void>;
  SaveDesktopLayout(layout: string, folderWidth: number, messageWidth: number): Promise<void>;
}

declare global {
  interface Window {
    go?: { main?: { DesktopAPI?: DesktopAPI } };
    runtime?: { EventsOn?: (name: string, callback: (...data: unknown[]) => void) => () => void };
  }
}

function api(): DesktopAPI {
  const bound = window.go?.main?.DesktopAPI;
  if (!bound && import.meta.env.DEV) return demoAPI;
  if (!bound) throw new Error("TideMail desktop bridge is unavailable. Start the app with Wails.");
  return bound;
}

const demoMessages: Message[] = [
  { ID: 1, MailboxID: 1, UID: 101, Subject: "The shoreline report", From: "Mira Chen <mira@example.com>", To: "you@example.com", CC: "", ReplyTo: "", Date: new Date().toISOString(), BodyText: "Morning—\n\nThe water is calm and the launch checklist is complete. I left the final notes in the shared folder.\n\nSee you by the pier,\nMira", Summary: "", Read: false, Starred: true, HasAttachment: true, Headers: "", MessageID: "demo-1", InReplyTo: "", References: "" },
  { ID: 2, MailboxID: 1, UID: 102, Subject: "Design review notes", From: "Theo Park <theo@example.com>", To: "you@example.com", CC: "", ReplyTo: "", Date: new Date(Date.now() - 86400000).toISOString(), BodyText: "The three-pane direction feels focused. A little more room in the reader will make long messages easier to scan.", Summary: "", Read: true, Starred: false, HasAttachment: false, Headers: "", MessageID: "demo-2", InReplyTo: "", References: "" },
  { ID: 3, MailboxID: 1, UID: 103, Subject: "Weekend tide tables", From: "Harbor Office <harbor@example.com>", To: "you@example.com", CC: "", ReplyTo: "", Date: new Date(Date.now() - 3 * 86400000).toISOString(), BodyText: "High tide arrives at 08:42 on Saturday. Conditions should remain clear through the afternoon.", Summary: "", Read: true, Starred: false, HasAttachment: false, Headers: "", MessageID: "demo-3", InReplyTo: "", References: "" },
];

const demoBootstrap: Bootstrap = {
  accounts: [{ ID: 1, Name: "Personal", Position: 1, Color: "#58a6ff" }, { ID: 2, Name: "Studio", Position: 2, Color: "#a371f7" }],
  mailboxes: [
    { ID: 1, AccountID: 1, Name: "INBOX", DisplayName: "Inbox", Flags: ["\\Inbox"], UnreadCount: 1 },
    { ID: 2, AccountID: 1, Name: "Archive", DisplayName: "Archive", Flags: ["\\Archive"], UnreadCount: 0 },
    { ID: 3, AccountID: 1, Name: "Drafts", DisplayName: "Drafts", Flags: ["\\Drafts"], UnreadCount: 0 },
    { ID: 4, AccountID: 2, Name: "INBOX", DisplayName: "Inbox", Flags: ["\\Inbox"], UnreadCount: 4 },
  ],
  config: { theme: "catppuccin-mocha", display: { desktopLayout: "native", desktopFolderWidth: 236, desktopMessageWidth: 390 }, accounts: [{ name: "Personal", user: "you@example.com", from: "You <you@example.com>" }], aiProvider: "openai" },
};

const demoAPI: DesktopAPI = {
  Bootstrap: async () => demoBootstrap,
  ListMessages: async () => ({ mailboxId: 1, messages: demoMessages }),
  Search: async (query) => ({ mailboxId: 0, query, messages: demoMessages.filter((message) => `${message.Subject} ${message.From} ${message.BodyText}`.toLowerCase().includes(query.toLowerCase())) }),
  SyncMailbox: async (mailboxId) => ({ mailboxId, fetched: 3, new: 0 }),
  Message: async (id) => ({ message: demoMessages.find((message) => message.ID === id)!, attachments: id === 1 ? [{ id: 1, filename: "launch-checklist.pdf", contentType: "application/pdf", size: 248320 }] : [] }),
  SetRead: async (id, read) => { const message = demoMessages.find((row) => row.ID === id); if (message) message.Read = read; },
  SetStarred: async (id, starred) => { const message = demoMessages.find((row) => row.ID === id); if (message) message.Starred = starred; },
  Archive: async () => undefined,
  Delete: async () => undefined,
  PickAttachments: async () => [{ name: "notes.txt", data: btoa("Demo attachment"), size: 15 }],
  SaveAttachment: async () => "/tmp/demo-attachment",
  Send: async () => undefined,
  Summarize: async () => "The launch checklist is complete, conditions are calm, and final notes are available in the shared folder.",
  Settings: async () => ({ accounts: [] }),
  SaveSettings: async () => undefined,
  SaveDesktopLayout: async () => undefined,
};

export const backend = {
  bootstrap: () => api().Bootstrap(),
  listMessages: (mailboxId: number, unified = false, unreadOnly = false) => api().ListMessages(mailboxId, unified, unreadOnly),
  search: (query: string) => api().Search(query),
  syncMailbox: (mailboxId: number) => api().SyncMailbox(mailboxId),
  message: (id: number) => api().Message(id),
  setRead: (id: number, read: boolean) => api().SetRead(id, read),
  setStarred: (id: number, starred: boolean) => api().SetStarred(id, starred),
  archive: (id: number) => api().Archive(id),
  delete: (id: number) => api().Delete(id),
  pickAttachments: () => api().PickAttachments(),
  saveAttachment: (messageId: number, attachmentId: number) => api().SaveAttachment(messageId, attachmentId),
  send: (request: ComposeRequest) => api().Send(request),
  summarize: (id: number) => api().Summarize(id),
  settings: () => api().Settings(),
  saveSettings: (settings: DesktopSettings) => api().SaveSettings(settings),
  saveDesktopLayout: (layout: "native" | "modern", folderWidth: number, messageWidth: number) => api().SaveDesktopLayout(layout, folderWidth, messageWidth),
  on: (name: string, callback: () => void) => window.runtime?.EventsOn?.(name, callback),
  onCommand: (callback: (command: string) => void) => window.runtime?.EventsOn?.("desktop.command", (...data) => callback(String(data[0] ?? ""))),
};
