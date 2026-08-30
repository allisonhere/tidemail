# Desktop Layouts

## Application shell

- Source: `cmd/tidemail-gui/frontend/src/App.tsx`
- Description: Single-route React/Wails mail client with top toolbar, folder
  sidebar, message list, reader, compose modal, and settings modal.

```tsx
export function App() {
  // Shared Wails bootstrap, mailbox, message, compose, sync, and settings state.
  return (
    <div className="app-shell">
      <header className="topbar">brand, global search, sync, settings, compose</header>
      <main className="workspace">
        <Sidebar />
        <section className="message-column">mailbox heading and message list</section>
        <Reader />
      </main>
      {/* ComposeModal and SettingsModal overlays */}
    </div>
  );
}
```

The full, authoritative implementation is 282 lines and is passed directly to
design generation as `cmd/tidemail-gui/frontend/src/App.tsx`; global styling is
passed as `cmd/tidemail-gui/frontend/src/styles.css`.
