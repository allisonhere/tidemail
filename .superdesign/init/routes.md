# Routes

TideMail is an embedded Wails desktop frontend without URL routing.

| Surface | Entry | Layout |
|---|---|---|
| Main mail window | `cmd/tidemail-gui/frontend/src/main.tsx` | `App` |

Mailbox, unified inbox, and search are stateful views within the same desktop
window. Compose and Settings are overlays owned by `App`.
