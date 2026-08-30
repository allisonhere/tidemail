import { CSSProperties, FormEvent, MouseEvent as ReactMouseEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Archive, ChevronDown, CircleAlert, Columns3, Inbox, Mail, Menu, Paperclip, PenLine, RefreshCw,
  Search, Send, Settings, Sparkles, Star, Trash2, X,
} from "lucide-react";
import { backend } from "./bridge";
import type { Account, AccountSettings, Bootstrap, ComposeRequest, DesktopSettings, Mailbox, Message, MessageDetail } from "./types";

type View = { kind: "unified" } | { kind: "mailbox"; mailbox: Mailbox } | { kind: "search"; query: string };

const emptyCompose: ComposeRequest = {
  accountName: "", to: [], cc: [], bcc: [], subject: "", body: "", inReplyTo: "", references: "", attachments: [],
};

export function App() {
  const [bootstrap, setBootstrap] = useState<Bootstrap>();
  const [view, setView] = useState<View>({ kind: "unified" });
  const [messages, setMessages] = useState<Message[]>([]);
  const [selectedId, setSelectedId] = useState<number>();
  const [detail, setDetail] = useState<MessageDetail>();
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [query, setQuery] = useState("");
  const [compose, setCompose] = useState<ComposeRequest>();
  const [sending, setSending] = useState(false);
  const [summary, setSummary] = useState("");
  const [sidebarOpen, setSidebarOpen] = useState(false);
	const [syncing, setSyncing] = useState(false);
  const [settings, setSettings] = useState<DesktopSettings>();
  const [contextMessage, setContextMessage] = useState<{ message: Message; x: number; y: number }>();
  const [layout, setLayout] = useState<"native" | "modern">("native");
  const [folderWidth, setFolderWidth] = useState(236);
  const [messageWidth, setMessageWidth] = useState(390);
  const searchRef = useRef<HTMLInputElement>(null);

  const loadMessages = useCallback(async (nextView: View = view) => {
    setLoading(true);
    setError("");
    try {
      const page = nextView.kind === "search"
        ? await backend.search(nextView.query)
        : await backend.listMessages(nextView.kind === "mailbox" ? nextView.mailbox.ID : 0, nextView.kind === "unified", false);
      const rows = page.messages ?? [];
      setMessages(rows);
      setSelectedId((current) => rows.some((message) => message.ID === current) ? current : rows[0]?.ID);
    } catch (reason) {
      setError(errorText(reason));
    } finally {
      setLoading(false);
    }
  }, [view]);

  useEffect(() => {
    backend.bootstrap().then((data) => {
      setBootstrap(data);
      setLayout(data.config.display?.desktopLayout ?? "native");
      setFolderWidth(data.config.display?.desktopFolderWidth || 236);
      setMessageWidth(data.config.display?.desktopMessageWidth || 390);
    }).catch((reason) => setError(errorText(reason)));
  }, []);

  useEffect(() => { void loadMessages(); }, [view]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!selectedId) { setDetail(undefined); return; }
    backend.message(selectedId).then(setDetail).catch((reason) => setError(errorText(reason)));
  }, [selectedId]);

  useEffect(() => {
    const off = backend.on("messages.changed", () => void loadMessages());
    return () => off?.();
  }, [loadMessages]);

  const openView = (next: View) => {
    setView(next);
    setSidebarOpen(false);
    setSummary("");
  };

  const search = (event: FormEvent) => {
    event.preventDefault();
    const trimmed = query.trim();
    if (trimmed) openView({ kind: "search", query: trimmed });
  };

	const syncCurrent = async () => {
		if (!bootstrap) return;
		setSyncing(true);
		setError("");
		try {
			const targets = view.kind === "mailbox"
				? [view.mailbox]
				: bootstrap.mailboxes.filter((mailbox) => mailbox.Name.toLowerCase() === "inbox" || mailbox.Flags?.some((flag) => flag.toLowerCase() === "\\inbox"));
			for (const mailbox of targets) await backend.syncMailbox(mailbox.ID);
			const next = await backend.bootstrap();
			setBootstrap(next);
			await loadMessages();
		} catch (reason) { setError(errorText(reason)); }
		finally { setSyncing(false); }
	};

  const mutate = async (work: () => Promise<void>) => {
    setError("");
    try { await work(); await loadMessages(); }
    catch (reason) { setError(errorText(reason)); }
  };

  const openCompose = (seed?: Partial<ComposeRequest>) => {
    setCompose({
      ...emptyCompose,
      accountName: bootstrap?.config.accounts?.[0]?.name ?? bootstrap?.accounts?.[0]?.Name ?? "",
      ...seed,
    });
  };

  const reply = () => {
    if (!detail) return;
    const message = detail.message;
    openCompose({
      to: [message.ReplyTo || message.From],
      subject: message.Subject.toLowerCase().startsWith("re:") ? message.Subject : `Re: ${message.Subject}`,
      inReplyTo: message.MessageID,
      references: `${message.References} ${message.MessageID}`.trim(),
      body: `\n\nOn ${formatDate(message.Date)}, ${message.From} wrote:\n${quote(message.BodyText)}`,
    });
  };

  const title = view.kind === "unified" ? "Unified inbox" : view.kind === "search" ? `Search: ${view.query}` : view.mailbox.DisplayName || view.mailbox.Name;

  const openSettings = async () => {
    setError("");
    try {
      const current = await backend.settings();
      setSettings(current.accounts.length ? current : { accounts: [newAccount()] });
    } catch (reason) { setError(errorText(reason)); }
  };

  const saveLayout = useCallback((next: "native" | "modern", folders = folderWidth, messagesWidth = messageWidth) => {
    setLayout(next);
    void backend.saveDesktopLayout(next, Math.round(folders), Math.round(messagesWidth)).catch((reason) => setError(errorText(reason)));
  }, [folderWidth, messageWidth]);

  const beginResize = (event: ReactMouseEvent, pane: "folders" | "messages") => {
    event.preventDefault();
    const startX = event.clientX;
    const start = pane === "folders" ? folderWidth : messageWidth;
    let finalValue = start;
    const move = (next: globalThis.MouseEvent) => {
      const value = Math.max(pane === "folders" ? 180 : 300, Math.min(pane === "folders" ? 360 : 560, start + next.clientX - startX));
      finalValue = value;
      if (pane === "folders") setFolderWidth(value); else setMessageWidth(value);
    };
    const done = () => {
      document.removeEventListener("mousemove", move);
      document.removeEventListener("mouseup", done);
      document.body.classList.remove("resizing-pane");
      void backend.saveDesktopLayout(layout, pane === "folders" ? finalValue : folderWidth, pane === "messages" ? finalValue : messageWidth);
    };
    document.body.classList.add("resizing-pane");
    document.addEventListener("mousemove", move);
    document.addEventListener("mouseup", done, { once: true });
  };

  const resizeWithKeyboard = (event: React.KeyboardEvent, pane: "folders" | "messages") => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const delta = event.key === "ArrowLeft" ? -10 : 10;
    const nextFolder = pane === "folders" ? Math.max(180, Math.min(360, folderWidth + delta)) : folderWidth;
    const nextMessage = pane === "messages" ? Math.max(300, Math.min(560, messageWidth + delta)) : messageWidth;
    setFolderWidth(nextFolder); setMessageWidth(nextMessage);
    void backend.saveDesktopLayout(layout, nextFolder, nextMessage);
  };

  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null;
      const editing = target?.matches("input, textarea, select, [contenteditable=true]");
      if ((event.ctrlKey || event.metaKey) && event.shiftKey && event.key.toLowerCase() === "l") { event.preventDefault(); saveLayout(layout === "native" ? "modern" : "native"); return; }
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "n") { event.preventDefault(); openCompose(); return; }
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "f") { event.preventDefault(); searchRef.current?.focus(); return; }
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "r") { event.preventDefault(); void syncCurrent(); return; }
      if (editing) return;
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault();
        const index = messages.findIndex((message) => message.ID === selectedId);
        const next = Math.max(0, Math.min(messages.length - 1, index + (event.key === "ArrowDown" ? 1 : -1)));
        if (messages[next]) setSelectedId(messages[next].ID);
        return;
      }
      if (!detail) return;
      if (event.key.toLowerCase() === "r") { event.preventDefault(); reply(); }
      if (event.key.toLowerCase() === "a") { event.preventDefault(); void mutate(() => backend.archive(detail.message.ID)); }
      if (event.key.toLowerCase() === "s") { event.preventDefault(); void mutate(() => backend.setStarred(detail.message.ID, !detail.message.Starred)); }
      if (event.key.toLowerCase() === "u") { event.preventDefault(); void mutate(() => backend.setRead(detail.message.ID, !detail.message.Read)); }
      if (event.key === "Delete") { event.preventDefault(); void mutate(() => backend.delete(detail.message.ID)); }
    };
    window.addEventListener("keydown", keydown);
    return () => window.removeEventListener("keydown", keydown);
  });

  useEffect(() => backend.onCommand((command) => {
    if (command === "compose") openCompose();
    if (command === "sync") void syncCurrent();
    if (command === "settings") void openSettings();
    if (command === "search") searchRef.current?.focus();
    if (command === "layout.native" || command === "layout.modern") saveLayout(command.endsWith("native") ? "native" : "modern");
    if (!detail) return;
    if (command === "reply") reply();
    if (command === "archive") void mutate(() => backend.archive(detail.message.ID));
    if (command === "star") void mutate(() => backend.setStarred(detail.message.ID, !detail.message.Starred));
    if (command === "read") void mutate(() => backend.setRead(detail.message.ID, !detail.message.Read));
    if (command === "delete") void mutate(() => backend.delete(detail.message.ID));
  }), [detail, saveLayout]);

  const workspaceStyle = { "--folder-width": `${folderWidth}px`, "--message-width": `${messageWidth}px` } as CSSProperties;

  return (
    <div className={`app-shell layout-${layout}`} style={workspaceStyle}>
      <header className="topbar">
        <button className="icon-button mobile-menu" onClick={() => setSidebarOpen((value) => !value)} aria-label="Toggle sidebar"><Menu /></button>
        <div className="brand"><span className="brand-mark">≈</span><span>TideMail</span><span className="preview-pill">desktop preview</span></div>
        <form className="search-box" onSubmit={search}>
          <Search size={16} /><input ref={searchRef} value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search all mail" aria-label="Search all mail" />
          <kbd>/</kbd>
        </form>
        <div className="top-actions">
          <button className="icon-button" onClick={() => void syncCurrent()} title="Sync mail"><RefreshCw className={syncing ? "spin" : ""} size={18} /></button>
          <button className="icon-button" title="Settings" aria-label="Open settings" onClick={() => void openSettings()}><Settings size={18} /></button>
          <button className="icon-button layout-toggle" title={`Switch to ${layout === "native" ? "Modern" : "Native"} layout (Ctrl+Shift+L)`} aria-label="Switch desktop layout" onClick={() => saveLayout(layout === "native" ? "modern" : "native")}><Columns3 size={18} /></button>
          <button className="compose-button" onClick={() => openCompose()}><PenLine size={16} />Compose</button>
        </div>
      </header>

      {error && <div className="error-banner"><CircleAlert size={16} /><span>{error}</span><button onClick={() => setError("")}><X size={14} /></button></div>}

      <main className="workspace">
        <Sidebar open={sidebarOpen} data={bootstrap} active={view} onSelect={openView} />
        <div className="pane-splitter folder-splitter" role="separator" tabIndex={layout === "native" ? 0 : -1} aria-label="Resize folders pane" aria-orientation="vertical" onKeyDown={(event) => resizeWithKeyboard(event, "folders")} onMouseDown={(event) => beginResize(event, "folders")} />
        <section className="message-column">
          <div className="pane-heading"><div><span className="eyebrow">MAILBOX</span><h1>{title}</h1></div><span className="message-count">{messages.length} messages</span></div>
          <div className="message-list" role="listbox" aria-label={title}>
            {loading && <Empty icon={<RefreshCw className="spin" />} title="Loading mail" copy="Reading the local cache…" />}
            {!loading && messages.length === 0 && <Empty icon={<Mail />} title="Nothing here" copy={view.kind === "search" ? "Try a broader search." : "Sync this mailbox to fetch mail."} />}
            {!loading && messages.map((message) => (
              <button key={message.ID} className={`message-row ${selectedId === message.ID ? "selected" : ""} ${message.Read ? "read" : "unread"}`} onContextMenu={(event) => { event.preventDefault(); setSelectedId(message.ID); setContextMessage({ message, x: event.clientX, y: event.clientY }); }} onClick={() => { setSelectedId(message.ID); setSummary(""); }} role="option" aria-selected={selectedId === message.ID}>
                <span className="unread-dot" />
                <span className="message-copy"><span className="sender">{senderName(message.From)}</span><span className="subject">{message.Subject || "(no subject)"}</span><span className="preview">{message.BodyText}</span></span>
                <span className="message-meta"><time>{relativeDate(message.Date)}</time>{message.HasAttachment && <Paperclip size={13} />}{message.Starred && <Star size={13} fill="currentColor" />}</span>
              </button>
            ))}
          </div>
        </section>
        <div className="pane-splitter message-splitter" role="separator" tabIndex={layout === "native" ? 0 : -1} aria-label="Resize message pane" aria-orientation="vertical" onKeyDown={(event) => resizeWithKeyboard(event, "messages")} onMouseDown={(event) => beginResize(event, "messages")} />

        <Reader detail={detail} summary={summary} onReply={reply} onSummary={async () => {
          if (!detail) return;
          try { setSummary("Generating summary…"); setSummary(await backend.summarize(detail.message.ID)); }
          catch (reason) { setSummary(""); setError(errorText(reason)); }
        }} onRead={(read) => detail && void mutate(() => backend.setRead(detail.message.ID, read))}
          onStar={(starred) => detail && void mutate(() => backend.setStarred(detail.message.ID, starred))}
          onArchive={() => detail && void mutate(() => backend.archive(detail.message.ID))}
          onDelete={() => detail && void mutate(() => backend.delete(detail.message.ID))}
          onSaveAttachment={(attachmentId) => detail && void backend.saveAttachment(detail.message.ID, attachmentId).catch((reason) => setError(errorText(reason)))} />
      </main>

      {compose && <ComposeModal value={compose} accounts={bootstrap?.accounts ?? []} sending={sending} onClose={() => setCompose(undefined)} onSend={async (request) => {
        setSending(true); setError("");
        try { await backend.send(request); setCompose(undefined); }
        catch (reason) { setError(errorText(reason)); }
        finally { setSending(false); }
      }} />}
      {settings && <SettingsModal value={settings} layout={layout} onLayout={saveLayout} onClose={() => setSettings(undefined)} onSave={async (next) => {
        setError("");
        try { await backend.saveSettings(next); setBootstrap(await backend.bootstrap()); setSettings(undefined); }
        catch (reason) { setError(errorText(reason)); throw reason; }
      }} />}
      {contextMessage && <div className="message-context-menu" style={{ left: contextMessage.x, top: contextMessage.y }} role="menu" onMouseLeave={() => setContextMessage(undefined)}>
        <button role="menuitem" onClick={() => { reply(); setContextMessage(undefined); }}>Reply <kbd>R</kbd></button>
        <button role="menuitem" onClick={() => { void mutate(() => backend.setStarred(contextMessage.message.ID, !contextMessage.message.Starred)); setContextMessage(undefined); }}>{contextMessage.message.Starred ? "Unstar" : "Star"} <kbd>S</kbd></button>
        <button role="menuitem" onClick={() => { void mutate(() => backend.setRead(contextMessage.message.ID, !contextMessage.message.Read)); setContextMessage(undefined); }}>{contextMessage.message.Read ? "Mark unread" : "Mark read"} <kbd>U</kbd></button>
        <button role="menuitem" onClick={() => { void mutate(() => backend.archive(contextMessage.message.ID)); setContextMessage(undefined); }}>Archive <kbd>A</kbd></button>
        <button className="danger" role="menuitem" onClick={() => { void mutate(() => backend.delete(contextMessage.message.ID)); setContextMessage(undefined); }}>Delete <kbd>Del</kbd></button>
      </div>}
    </div>
  );
}

function newAccount(): AccountSettings {
  return { name: "", provider: "", imap_host: "", imap_port: 993, imap_tls: true, smtp_host: "", smtp_port: 587, smtp_tls: true, user: "", password: "", from: "", sync_minutes: 5, signature: "", refresh_token: "" };
}

function SettingsModal({ value, layout, onLayout, onClose, onSave }: { value: DesktopSettings; layout: "native" | "modern"; onLayout(value: "native" | "modern"): void; onClose(): void; onSave(value: DesktopSettings): Promise<void> }) {
  const [draft, setDraft] = useState(value);
  const [selected, setSelected] = useState(0);
  const [saving, setSaving] = useState(false);
  const account = draft.accounts[selected];
  const update = <K extends keyof AccountSettings>(key: K, next: AccountSettings[K]) => {
    const accounts = [...draft.accounts]; accounts[selected] = { ...account, [key]: next }; setDraft({ accounts });
  };
  const submit = async (event: FormEvent) => { event.preventDefault(); setSaving(true); try { await onSave(draft); } finally { setSaving(false); } };
  return <div className="modal-backdrop settings-backdrop" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
    <form className="settings-modal" onSubmit={(event) => void submit(event)}>
      <div className="compose-header"><div><span className="eyebrow">SETTINGS</span><strong>Email accounts</strong></div><button type="button" className="icon-button" onClick={onClose}><X /></button></div>
      <div className="settings-layout">
        <aside className="settings-accounts"><div className="settings-layout-picker"><span>Desktop layout</span><button type="button" className={layout === "native" ? "active" : ""} onClick={() => onLayout("native")}>Native</button><button type="button" className={layout === "modern" ? "active" : ""} onClick={() => onLayout("modern")}>Modern</button></div>{draft.accounts.map((item, index) => <button type="button" className={selected === index ? "active" : ""} key={index} onClick={() => setSelected(index)}>{item.name || "New account"}</button>)}<button type="button" onClick={() => { setDraft({ accounts: [...draft.accounts, newAccount()] }); setSelected(draft.accounts.length); }}>+ Add account</button></aside>
        {account && <div className="settings-form">
          <label>Account name<input required value={account.name} onChange={(e) => update("name", e.target.value)} placeholder="Personal" /></label>
          <label>Email / username<input required value={account.user} onChange={(e) => update("user", e.target.value)} placeholder="you@example.com" /></label>
          <label>From address<input value={account.from} onChange={(e) => update("from", e.target.value)} placeholder="You &lt;you@example.com&gt;" /></label>
          <label>Password<input type="password" value={account.password} onChange={(e) => update("password", e.target.value)} placeholder="App password" /></label>
          <div className="settings-section-title">Incoming mail (IMAP)</div>
          <label>Server<input required value={account.imap_host} onChange={(e) => update("imap_host", e.target.value)} placeholder="imap.example.com" /></label>
          <label>Port<input required type="number" value={account.imap_port} onChange={(e) => update("imap_port", Number(e.target.value))} /></label>
          <label className="check">Use TLS<input type="checkbox" checked={account.imap_tls} onChange={(e) => update("imap_tls", e.target.checked)} /></label>
          <div className="settings-section-title">Outgoing mail (SMTP)</div>
          <label>Server<input required value={account.smtp_host} onChange={(e) => update("smtp_host", e.target.value)} placeholder="smtp.example.com" /></label>
          <label>Port<input required type="number" value={account.smtp_port} onChange={(e) => update("smtp_port", Number(e.target.value))} /></label>
          <label className="check">Use TLS<input type="checkbox" checked={account.smtp_tls} onChange={(e) => update("smtp_tls", e.target.checked)} /></label>
        </div>}
      </div>
      <div className="settings-footer"><span>Credentials use TideMail’s secure configuration flow.</span><button type="button" className="text-button" onClick={onClose}>Cancel</button><button className="send-button" disabled={saving}>{saving ? "Saving…" : "Save settings"}</button></div>
    </form>
  </div>;
}

function Sidebar({ open, data, active, onSelect }: { open: boolean; data?: Bootstrap; active: View; onSelect: (view: View) => void }) {
  const grouped = useMemo(() => (data?.accounts ?? []).map((account) => ({ account, mailboxes: (data?.mailboxes ?? []).filter((mailbox) => mailbox.AccountID === account.ID) })), [data]);
  return <aside className={`sidebar ${open ? "open" : ""}`}>
    <nav>
      <button className={active.kind === "unified" ? "nav-row active" : "nav-row"} onClick={() => onSelect({ kind: "unified" })}><Inbox size={16} /><span>Unified inbox</span></button>
      {grouped.map(({ account, mailboxes }) => <div className="account-group" key={account.ID}>
        <div className="account-heading"><span className="account-dot" style={{ background: account.Color || "#58a6ff" }} /><span>{account.Name}</span><ChevronDown size={13} /></div>
        {mailboxes.map((mailbox) => <button key={mailbox.ID} className={active.kind === "mailbox" && active.mailbox.ID === mailbox.ID ? "nav-row active" : "nav-row"} onClick={() => onSelect({ kind: "mailbox", mailbox })}><span className="nav-indent" />{mailbox.DisplayName || mailbox.Name}<span className="badge">{mailbox.UnreadCount || ""}</span></button>)}
      </div>)}
    </nav>
    <div className="sidebar-footer"><span className="online-dot" />Local cache ready</div>
  </aside>;
}

function Reader({ detail, summary, onReply, onSummary, onRead, onStar, onArchive, onDelete, onSaveAttachment }: {
  detail?: MessageDetail; summary: string; onReply(): void; onSummary(): void; onRead(read: boolean): void; onStar(starred: boolean): void; onArchive(): void; onDelete(): void; onSaveAttachment(id: number): void;
}) {
  if (!detail) return <section className="reader"><Empty icon={<Mail />} title="Choose a message" copy="The reading pane keeps your place in the inbox." /></section>;
  const { message, attachments } = detail;
  return <section className="reader">
    <div className="reader-toolbar">
      <button onClick={onReply}>Reply</button><button onClick={onArchive}><Archive size={15} />Archive</button><button onClick={() => onStar(!message.Starred)}><Star size={15} fill={message.Starred ? "currentColor" : "none"} />{message.Starred ? "Unstar" : "Star"}</button><button onClick={() => onRead(!message.Read)}>{message.Read ? "Mark unread" : "Mark read"}</button><button className="danger" onClick={onDelete}><Trash2 size={15} />Delete</button>
    </div>
    <article className="reader-scroll">
      <div className="message-header"><div className="avatar">{senderName(message.From).slice(0, 1).toUpperCase()}</div><div className="message-title"><h2>{message.Subject || "(no subject)"}</h2><p><strong>{senderName(message.From)}</strong> <span>{message.From}</span></p><p className="recipient">to {message.To || "me"}</p></div><time>{formatDate(message.Date)}</time></div>
      <div className="reader-actions"><button className="ai-button" onClick={onSummary}><Sparkles size={15} />Summarize</button></div>
      {summary && <aside className="summary-card"><span><Sparkles size={15} />AI SUMMARY</span><p>{summary}</p></aside>}
      <div className="message-body">{message.BodyText || "This message has no readable text body."}</div>
      {attachments.length > 0 && <div className="attachment-section"><h3><Paperclip size={15} />Attachments</h3>{attachments.map((attachment) => <button className="attachment" key={attachment.id} onClick={() => onSaveAttachment(attachment.id)}><Paperclip size={16} /><span>{attachment.filename}</span><small>{formatBytes(attachment.size)}</small></button>)}</div>}
    </article>
  </section>;
}

function ComposeModal({ value, accounts, sending, onClose, onSend }: { value: ComposeRequest; accounts: Account[]; sending: boolean; onClose(): void; onSend(value: ComposeRequest): void }) {
  const [draft, setDraft] = useState(value);
  const address = (field: "to" | "cc" | "bcc", text: string) => setDraft({ ...draft, [field]: text.split(",").map((item) => item.trim()).filter(Boolean) });
  return <div className="modal-backdrop compose-backdrop" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
    <form className="compose-modal" onSubmit={(event) => { event.preventDefault(); onSend(draft); }}>
      <div className="compose-header"><div><span className="eyebrow">NEW MESSAGE</span><select value={draft.accountName} onChange={(event) => setDraft({ ...draft, accountName: event.target.value })}>{accounts.map((account) => <option key={account.ID}>{account.Name}</option>)}</select></div><button type="button" className="icon-button" onClick={onClose}><X /></button></div>
      <div className="compose-fields"><label>To<input autoFocus required value={draft.to.join(", ")} onChange={(event) => address("to", event.target.value)} placeholder="name@example.com" /></label><label>CC<input value={draft.cc.join(", ")} onChange={(event) => address("cc", event.target.value)} /></label><label>Subject<input value={draft.subject} onChange={(event) => setDraft({ ...draft, subject: event.target.value })} placeholder="What’s this about?" /></label></div>
      <textarea value={draft.body} onChange={(event) => setDraft({ ...draft, body: event.target.value })} placeholder="Write your message…" />
      {draft.attachments.length > 0 && <div className="compose-attachments">{draft.attachments.map((attachment, index) => <span key={`${attachment.name}-${index}`}><Paperclip size={13} />{attachment.name}<button type="button" onClick={() => setDraft({ ...draft, attachments: draft.attachments.filter((_, item) => item !== index) })}><X size={12} /></button></span>)}</div>}
      <div className="compose-footer"><button type="button" className="text-button" onClick={async () => setDraft({ ...draft, attachments: [...draft.attachments, ...await backend.pickAttachments()] })}><Paperclip size={16} />Attach</button><span>Plain text · Ctrl+Enter to send</span><button className="send-button" disabled={sending || draft.to.length === 0}><Send size={16} />{sending ? "Sending…" : "Send"}</button></div>
    </form>
  </div>;
}

function Empty({ icon, title, copy }: { icon: React.ReactNode; title: string; copy: string }) { return <div className="empty-state"><div className="empty-icon">{icon}</div><h2>{title}</h2><p>{copy}</p></div>; }
function errorText(reason: unknown) { return reason instanceof Error ? reason.message : String(reason); }
function senderName(value: string) { return value.match(/^\s*"?([^"<]+)"?\s*</)?.[1]?.trim() || value.split("@")[0] || "Unknown sender"; }
function relativeDate(value: string) { const date = new Date(value); const days = Math.floor((Date.now() - date.getTime()) / 86400000); return days < 1 ? date.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" }) : days === 1 ? "Yesterday" : days < 7 ? date.toLocaleDateString([], { weekday: "short" }) : date.toLocaleDateString([], { month: "short", day: "numeric" }); }
function formatDate(value: string) { return new Date(value).toLocaleString([], { dateStyle: "medium", timeStyle: "short" }); }
function quote(value: string) { return value.split("\n").map((line) => `> ${line}`).join("\n"); }
function formatBytes(value: number) { return value < 1024 ? `${value} B` : value < 1048576 ? `${(value / 1024).toFixed(1)} KB` : `${(value / 1048576).toFixed(1)} MB`; }
