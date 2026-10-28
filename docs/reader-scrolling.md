# Reader scrolling validation

Reader-only Up/Down movements and wheel events reuse the unchanged sidebar and
message-list output. The reader pane and status line remain live. Every other
event invalidates these pane strings, including mail updates, resize, settings,
theme changes, pane changes, and overlay transitions. Existing body and viewport
caches remain independent. No input is discarded, delayed, or coalesced.

Run the real-event diagnostic:

```sh
go test ./internal/ui -run '^TestReaderScrollEventQueueDrain$' -count=1 -v
```

It feeds 60 complete arrow/SGR-wheel sequences at 30 Hz or 120 Hz through Bubble
Tea's decoder, event loop, full views and standard renderer. Cases cover 21 KB
and 333 KB HTML newsletters, 120×40, 160×50 and 80×24 terminals, both navigation
directions and both highlight settings. Every event's navigation state must
match a sequential reference, and delivered/processed/view counts must agree.
Fixture construction and initial layout are excluded from the reported run time.

Logs include mean/p95/p99 Update, View, combined cycle and arrival-to-view
latency, total elapsed time, and post-release Update and final-view drain.
Percentiles use nearest rank; with 60 samples p99 is the largest sample.
`-short` skips the paced diagnostic. On a controlled performance runner,
`TIDEMAIL_READER_PERF_LIMITS=1` additionally enforces a 20 ms p95 cycle and 50 ms
final-view drain for 30 Hz cases. These timing limits are opt-in to avoid noisy CI
failures; input counts and navigation assertions always apply.

The original investigation was reproduced on the development machine before
pane reuse: at 120×40, a 21 KB newsletter averaged 39.26 ms per View and left
561 ms of final-view backlog after 60 Down events at 30 Hz. The 333 KB fixture
at 160×50 averaged 33.22 ms per View and left 142 ms of backlog.

A matched comparison used identical final navigation behavior and fixtures,
with only pane reuse disabled for the baseline. The measured baseline used a
temporary Go overlay that invalidated these panes on every event. The same
baseline is reproducible with the test-only flag (no production setting):

```sh
TIDEMAIL_READER_NO_PANE_REUSE=1 go test ./internal/ui -run '^TestReaderScrollEventQueueDrain$' -count=1 -v
```

Local results below show combined Update+View latency in milliseconds, final-view
post-release drain in milliseconds, and total run time in seconds. Fixture sizes
are HTML source sizes. Each baseline and final case processed all 60 delivered
events and matched every sequential navigation transition. Runs were sequential
on the same development machine; these are observations, not timing guarantees.

| Scenario | Baseline mean / p95 / p99 ms | Reuse mean / p95 / p99 ms | Drain ms, before → after | Total s, before → after |
|---|---:|---:|---:|---:|
| 21 KB, 120x40, Down, 30 Hz | 41.4 / 46.5 / 50.4 | 18.9 / 25.1 / 28.4 | 577.0 → 21.7 | 2.566 → 2.022 |
| 21 KB, 120x40, wheel Down, 30 Hz | 43.6 / 48.9 / 55.2 | 20.6 / 24.9 / 28.8 | 714.0 → 26.4 | 2.706 → 2.026 |
| 21 KB, 120x40, wheel Down, 120 Hz | 44.3 / 49.9 / 56.1 | 15.0 / 17.6 / 20.3 | 2227.8 → 450.9 | 2.739 → 0.963 |
| 333 KB, 160x50, Down, 30 Hz | 36.9 / 42.5 / 46.7 | 19.5 / 23.8 / 26.0 | 293.6 → 17.9 | 2.283 → 2.020 |
| 333 KB, 160x50, Up, 30 Hz | 39.1 / 45.6 / 50.5 | 19.1 / 22.9 / 23.9 | 428.6 → 22.3 | 2.417 → 2.028 |
| 333 KB, 160x50, wheel Up, 30 Hz | 37.0 / 42.2 / 44.7 | 19.8 / 22.8 / 25.5 | 307.0 → 21.5 | 2.291 → 2.020 |
| 333 KB, 160x50, wheel Down, 120 Hz | 38.0 / 44.0 / 47.9 | 13.2 / 16.5 / 17.9 | 1841.3 → 329.8 | 2.354 → 0.847 |
| 333 KB, 80x24, Down, 30 Hz | 26.3 / 31.2 / 37.0 | 11.0 / 13.5 / 14.2 | 30.8 → 10.7 | 2.011 → 1.995 |
| 333 KB, 80x24, wheel Down, 120 Hz | 24.2 / 28.5 / 30.3 | 8.9 / 10.9 / 12.6 | 1001.6 → 69.1 | 1.505 → 0.573 |
| 333 KB, 160x50, Down, 30 Hz (highlight off) | 38.5 / 43.9 / 45.4 | 18.3 / 22.6 / 22.9 | 393.3 → 22.4 | 2.383 → 2.028 |
| 333 KB, 160x50, wheel Down, 120 Hz (highlight off) | 39.2 / 45.9 / 46.9 | 13.3 / 17.8 / 18.4 | 1910.3 → 333.2 | 2.431 → 0.848 |

At 30 Hz, all final-view drain measurements were below 50 ms (11–26 ms), without
growing backlog. Wide-window p95 cycles were 23–25 ms, so the 20 ms cycle target
is only met in the narrow case. At 120 Hz, wide windows still accumulated
330–451 ms of backlog; the narrow case accumulated 69 ms. Remaining frame
construction/terminal work warrants separate profiling if high-rate input is a
priority; asynchronous rendering is not introduced here.

## Focus continuity and safety

Wheel movement remains three rows per event. Visible reading focus is retained;
otherwise reconciliation chooses the nearest visible focusable edge without
moving the viewport. A blank/header-only viewport keeps a visible pending anchor
and advances one row per subsequent arrow event until focusable content appears.
Resize retains the viewport offset where possible and reconciles the new layout.
Explicit LINKS navigation still reveals its selected target.

Focus-line highlighting is purely presentational. Both modes use identical
internal position, focusability, selection and arrow navigation. Automatic
reconciliation cannot establish external-link activation intent. Wheel scrolling
clears deliberate link selection and its chevron; only a subsequent deliberate
selection or keyboard focus can arm a target.

The performance commits are integrated on upstream main at `de13370`, which
includes the merged link-activation safeguards, destination display, header
scrolling fix, and hidden link counter until deliberate selection. The original
performance branch is unchanged.

## Terminal validation and limitations

A scripted interactive PTY replay with real Bubble Tea terminal input/output
processed all 11 arrow events and 14 wheel reports, with zero browser activations.
The fixture used isolated synthetic mail, not the user's configuration/database.
To run this opt-in terminal fixture, scroll/navigate and press `q` to exit:

```sh
TIDEMAIL_READER_INTERACTIVE=1 go test ./internal/ui -run '^TestReaderScrollInteractive$' -count=1 -v
```

This checks terminal rendering and event processing, not physical trackpad
momentum or perceived smoothness. Actual mouse/trackpad feel still requires manual
verification with the standalone application build. The three-row wheel increment
is intentionally unchanged; evaluate sensitivity only after that check.

During an uninterrupted navigation-only sequence, relative ages in the cached
message list can remain unchanged until the next invalidating event. Incoming
mail and all non-navigation events refresh the panes normally.
