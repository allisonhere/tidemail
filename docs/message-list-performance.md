# Message-list Model-copy performance

This optimization changes read-only helper receivers, reducing copies of the
large UI Model through message-list, sidebar, status, reader-frame and geometry
helpers. `Model.Init`, `Update` and `View` retain their value receivers and Bubble
Tea interface. The audited helper bodies are unchanged and do not write Model
value fields. Existing pointer-owned caches keep their original behavior.

There is no change to selection, shortcuts, layout, link activation, preview
preparation or cache policy. Preview preparation is still synchronous on every
selection. This phase adds no settling timer, background preview worker or frame
pacing. README, guide, status and help text need no behavior updates.

## Reproduce

```sh
go test ./internal/ui -run '^TestRenderHelpersPreserveModelValueState$' -count=1
go test ./internal/ui -run '^$' -bench '^BenchmarkMessageListView$' -benchmem -benchtime=30x -count=2
TIDEMAIL_LIST_PERF=1 go test ./internal/ui -run '^TestMessageListNavigationEventQueueDrain$' -count=1 -v
```

The fixed-count View benchmark warms the layout and excludes construction and
initial preview preparation. The event diagnostic is opt-in and skipped by
`-short`; it imposes no machine-specific timing gates on ordinary CI. It uses
complete arrow/SGR-wheel packets through Bubble Tea's decoder, real Update/View
and standard renderer, with a buffered producer independent of processing speed.
Every cursor transition, event/View count and final preview identity is checked.
Logs contain JSON timing metrics and a final ANSI frame hash for matched output
comparisons. The isolated synthetic fixtures never use real account credentials
or network servers.

## Matched observations

Measured on 2026-10-28 against unchanged upstream `7681235` using the identical
test file injected into the control with a Go overlay. This upstream baseline
already includes the reader-scrolling and internal mailto changes. Runs were
sequential in upstream/Phase 1/Phase 1/upstream order after other validation jobs
finished: Linux/amd64, Intel Core 5 320, six CPUs, Go 1.27.3-X:nodwarf5, TrueColor,
default concurrency and GC. These are development-machine observations, not
performance guarantees.

### Allocations per warmed View

Means of two fixed 30-iteration runs; MB is decimal. Model size is unchanged at
350,272 bytes (about 342 KiB).

| Terminal | Upstream bytes/View | Phase 1 bytes/View | Reduction |
|---|---:|---:|---:|
| 80x24 | 80,184,484 (80.2 MB) | 10,167,061 (10.2 MB) | 87.3% |
| 160x50 | 127,418,498 (127.4 MB) | 14,607,699 (14.6 MB) | 88.5% |

### Navigation latency and release backlog

Update and View cells are **mean / p95 / p99 milliseconds from the second run**;
percentiles are nearest-rank within that run, not averaged or pooled across
runs. Drain cells give the range across both runs for the time from last packet
release to the final selected frame being constructed. The separate renderer
write timestamp, cycle/arrival metrics and exact counts are logged as well.
Terminal writes do not measure physical display latency or real TTY backpressure.

Plain cases use 60 navigation events; cold ~21 KB HTML cases use 30. A warm
12-event reverse case fits the existing viewport cache. Widths are 80x24 and
160x50; primary 120 Hz cases use Unified Inbox and 30 Hz cases use an account
folder. The warm revisit control also uses an account folder.
Wheel movement remains three rows. Mouse support, headers, sender and actionable
links are enabled; threading, images and MarkReadOnFocus are disabled to isolate
navigation/rendering costs. Fixture construction, initial loading and the first
preview are excluded from timing. Newly selected HTML bodies still incur cold
conversion in Update; View-only allocation measurements are warmed.

| Scenario | Upstream Update | Phase 1 Update | Upstream View | Phase 1 View | Drain upstream → Phase 1 ms |
|---|---:|---:|---:|---:|---:|
| plain-80x24-key30 | 10.2 / 12.4 / 13.1 | 5.8 / 7.4 / 10.1 | 24.5 / 28.3 / 30.5 | 8.5 / 10.4 / 11.8 | 153.0–229.1 → 17.0–18.9 |
| plain-80x24-key120 | 9.9 / 11.9 / 12.7 | 3.9 / 5.7 / 7.6 | 24.8 / 29.1 / 30.8 | 9.0 / 11.9 / 13.5 | 1622.1–1632.7 → 320.1–375.0 |
| plain-160x50-key30 | 10.3 / 11.8 / 13.5 | 5.7 / 7.1 / 8.6 | 40.9 / 45.6 / 45.9 | 13.8 / 16.4 / 18.3 | 1166.5–1201.1 → 19.6–19.6 |
| plain-160x50-key120 | 10.0 / 12.0 / 14.6 | 4.0 / 5.4 / 7.8 | 41.8 / 46.0 / 53.2 | 15.8 / 18.9 / 21.4 | 2682.0–2792.8 → 741.9–791.9 |
| html21KB-80x24-key30 | 67.4 / 82.1 / 87.7 | 57.1 / 68.0 / 72.5 | 18.9 / 24.1 / 26.0 | 3.8 / 5.3 / 7.7 | 1654.0–1686.2 → 805.5–869.8 |
| html21KB-80x24-key120 | 65.7 / 72.7 / 77.2 | 57.2 / 63.5 / 64.5 | 18.4 / 22.3 / 25.3 | 4.2 / 5.8 / 6.0 | 2267.2–2313.2 → 1610.8–1613.0 |
| html21KB-160x50-key30 | 105.6 / 121.0 / 124.9 | 91.8 / 109.3 / 112.5 | 31.1 / 36.1 / 38.8 | 8.1 / 10.4 / 10.5 | 3146.6–3180.5 → 2045.1–2140.0 |
| html21KB-160x50-key120 | 101.1 / 124.8 / 133.0 | 93.6 / 109.2 / 109.8 | 32.5 / 39.4 / 44.5 | 9.5 / 12.3 / 12.6 | 3819.4–3866.6 → 2868.8–2888.3 |
| plain-wide-wheel120 | 12.4 / 14.4 / 16.7 | 2.6 / 3.4 / 4.1 | 44.4 / 49.7 / 53.2 | 11.1 / 14.9 / 15.3 | 2931.2–2978.8 → 358.2–369.0 |
| html-warm-up120 | 7.1 / 9.9 / 9.9 | 4.4 / 5.6 / 5.6 | 30.4 / 35.2 / 35.2 | 9.3 / 11.0 / 11.0 | 388.7–411.8 → 73.2–83.1 |

All 40 scenario runs processed all 1,728 events, and all final ANSI frame hashes
matched upstream. At 30 Hz, wide plain-text release drain fell from about 1.2 s
to 20 ms. **This does not solve cold HTML navigation or all 120 Hz backlog.**
Cold wide HTML still spends roughly 92–94 ms per Update and leaves seconds of
queued navigation. Plain wide 120 Hz input also still outpaces full Update/View.
Existing cold conversion, residual copies and frame construction remain separate
work; no later-phase mechanism is included here.

## Safety and scope

Pointer receivers were chosen over a new render context to preserve the existing
call graph and avoid duplicating display state. Only the measured read-only chain
was changed. Action/account-reload helpers (`hasSelection`, `currentRowMessages`,
`currentSidebarSelection`) from the experimental list remain value receivers;
`renderPaneHint` and `mailboxByID` also retain their existing value-callable APIs.
This is a reduction in copying, not a smaller Model or zero-allocation rendering.

Regression tests check repeated rendering and preservation of config/styles,
viewport, focus/cursors, search and link-selection state across narrow/wide,
normal/plain-theme and reader-search states. Existing pointer-owned render caches
are intentionally excluded from the value-state snapshot. Full UI/navigation,
reader/link, mailto, theme, account and race tests remain applicable. No mutation,
action-selection, concurrency, read/unread or cache-invalidation behavior is
intentionally changed. Future edits to these read-only helpers must not start
mutating Model value fields simply because the receiver is now a pointer.
