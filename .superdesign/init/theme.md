# Theme

## Compact token summary

- Font: Inter/system UI stack; message and compose bodies use system monospace.
- Backgrounds: `#0d1117`, `#11161d`, `#161c24`, `#1b222c`.
- Text: `#e6edf3`; muted `#8b98a7`; faint `#53606e`.
- Accent: `#58a6ff`; danger `#ff7b72`.
- Borders: `#28313d` and `#202832`.
- Radius: 6–14px; current modals use 13px.
- Shadow: `0 22px 80px #0008`.
- Breakpoints: 1100px and 900px.
- Current structure: 58px topbar; 240px folder pane; 330–410px message pane;
  flexible reader pane.

## Raw source

The complete raw stylesheet is under 900 lines and is passed directly on every
design call as `cmd/tidemail-gui/frontend/src/styles.css`.

```css
:root {
  font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
  --bg: #0d1117;
  --panel: #11161d;
  --panel-raised: #161c24;
  --panel-soft: #1b222c;
  --line: #28313d;
  --line-soft: #202832;
  --text: #e6edf3;
  --muted: #8b98a7;
  --faint: #53606e;
  --accent: #58a6ff;
  --accent-soft: #58a6ff1c;
  --danger: #ff7b72;
  --shadow: 0 22px 80px #0008;
}
```
