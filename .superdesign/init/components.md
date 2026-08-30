# Shared UI Components

The current desktop frontend has no component-library directory. Reusable UI is
implemented as local functions inside `cmd/tidemail-gui/frontend/src/App.tsx`.

```tsx
function Empty({ icon, title, copy }: { icon: React.ReactNode; title: string; copy: string }) {
  return <div className="empty-state"><div className="empty-icon">{icon}</div><h2>{title}</h2><p>{copy}</p></div>;
}
```

The other local components are `Sidebar`, `Reader`, `ComposeModal`, and
`SettingsModal`; their complete source is captured in `layouts.md`.
