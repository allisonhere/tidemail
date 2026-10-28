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

## Measured results

The final implementation was measured on 2026-10-28 after restoring one-row
keyboard scrolling with Focus line off. Both runs used the same navigation
behavior and fixtures; only pane reuse was disabled for the baseline using this
test-only flag (no production setting):

```sh
TIDEMAIL_READER_NO_PANE_REUSE=1 go test ./internal/ui -run '^TestReaderScrollEventQueueDrain$' -count=1 -v
```

Baseline and reuse runs were sequential on the same Linux/amd64 development
machine (Intel Core 5 320, Go 1.27.3), after other validation jobs finished.
Fixture sizes are HTML source sizes. Each of the 11 cases processed all 60
delivered events and matched every sequential navigation transition in both
runs. These are local observations, not performance guarantees.

The table reports combined Update+View latency, final-view post-release drain,
and total run time. Focus line is on unless marked off.

| Scenario | Baseline mean / p95 / p99 ms | Reuse mean / p95 / p99 ms | Drain ms, before → after | Total s, before → after |
|---|---:|---:|---:|---:|
| 21 KB, 120x40, Down, 30 Hz | 43.7 / 49.6 / 52.6 | 19.0 / 23.5 / 24.7 | 686.9 → 19.5 | 2.679 → 2.020 |
| 21 KB, 120x40, wheel Down, 30 Hz | 44.8 / 50.8 / 53.1 | 21.7 / 25.7 / 27.3 | 766.0 → 25.8 | 2.755 → 2.030 |
| 21 KB, 120x40, wheel Down, 120 Hz | 45.9 / 50.3 / 52.5 | 14.3 / 18.2 / 23.0 | 2305.4 → 412.7 | 2.822 → 0.925 |
| 333 KB, 160x50, Down, 30 Hz | 35.6 / 42.2 / 44.0 | 18.9 / 22.5 / 24.7 | 210.0 → 20.8 | 2.199 → 2.024 |
| 333 KB, 160x50, Up, 30 Hz | 36.6 / 42.2 / 46.6 | 18.9 / 22.6 / 25.0 | 264.2 → 21.0 | 2.255 → 2.025 |
| 333 KB, 160x50, wheel Up, 30 Hz | 35.5 / 39.8 / 43.0 | 20.3 / 23.8 / 25.6 | 190.3 → 20.7 | 2.178 → 2.027 |
| 333 KB, 160x50, wheel Down, 120 Hz | 35.5 / 40.4 / 42.4 | 14.5 / 18.0 / 18.9 | 1664.2 → 408.4 | 2.185 → 0.922 |
| 333 KB, 80x24, Down, 30 Hz | 26.8 / 31.9 / 34.2 | 12.2 / 16.1 / 19.4 | 26.8 → 16.7 | 2.011 → 2.005 |
| 333 KB, 80x24, wheel Down, 120 Hz | 25.0 / 28.5 / 30.7 | 8.9 / 11.7 / 14.1 | 1028.4 → 68.8 | 1.537 → 0.579 |
| 333 KB, 160x50, Down, 30 Hz (Focus line off) | 35.4 / 40.9 / 42.6 | 18.9 / 21.9 / 25.2 | 184.9 → 21.1 | 2.178 → 2.025 |
| 333 KB, 160x50, wheel Down, 120 Hz (Focus line off) | 35.7 / 40.8 / 41.9 | 13.4 / 17.8 / 20.7 | 1672.9 → 341.7 | 2.186 → 0.858 |

At 30 Hz, final-view drain was 17–26 ms. Wide-window p95 cycles were 22–26 ms,
above the optional 20 ms target; the narrow case met that target. At 120 Hz,
wide windows still accumulated 342–413 ms of backlog; the narrow case accumulated
69 ms. Remaining frame construction and terminal work warrant separate profiling
if high-rate input is a priority; asynchronous rendering is not introduced here.

Additional reader benchmarks can be reproduced with:

```sh
go test ./internal/ui -run '^$' -bench 'Benchmark(RenderMessageForDisplay|SetViewportThread|ReaderLinkNavigation)$' -benchmem -count=1
```

The same validation run measured link navigation at 28.1 ms/op for 21 KB and
53.4 ms/op for 333 KB. Rendering benchmarks mix cold rendering and cached
iterations: the 333 KB rendering case ran only one iteration, so it does not
support a stable comparison with runs using more iterations. The matched event
measurements above directly assess pane reuse during scrolling.

## Focus continuity and safety

Wheel movement remains three rows per event. Visible reading focus is retained;
otherwise reconciliation chooses the nearest visible focusable edge without
moving the viewport. In focus-enabled mode, a blank/header-only viewport keeps
a visible pending anchor and advances one row per arrow until focusable content
appears.
Resize retains the viewport offset where possible and reconciles the new layout.
Explicit LINKS navigation still reveals its selected target.

With Focus line enabled, arrows move the visible reading highlight and keep it
in view. With it disabled, ordinary arrows scroll exactly one row regardless of
internal focus. Reaching the bottom does not select a link: a further Down selects
the first LINKS entry (immediately for a message that fits). Down advances without
wrapping; Up from the first entry clears selection without scrolling, and the
next Up scrolls one row. Without actionable links, arrows only scroll. Partially
visible LINKS entries do not change this transition. Direct link shortcuts and
clicks establish the same explicit selection, from which arrows navigate normally.
Selected LINKS entries retain their existing local highlight and chevron in both
modes; disabling Focus line never adds a full-width reading highlight. Automatic
reconciliation cannot establish external-link activation intent. Wheel scrolling
clears deliberate link selection and its chevron; only a subsequent deliberate
selection or keyboard focus can arm a target.

## Terminal validation and limitations

The opt-in terminal fixture uses isolated synthetic mail rather than the user's
configuration or database. To run it, scroll/navigate and press `q` to exit:

```sh
TIDEMAIL_READER_INTERACTIVE=1 go test ./internal/ui -run '^TestReaderScrollInteractive$' -count=1 -v
```

The fixture checks terminal rendering and event processing, not physical
trackpad momentum or perceived smoothness. Manual testing in Foot and Kitty,
including messages with images, confirmed improved scrolling and correct
behavior in both Focus line modes. Other terminals and input devices still
require manual verification. The three-row wheel increment is unchanged.

During an uninterrupted navigation-only sequence, relative ages in the cached
message list can remain unchanged until the next invalidating event. Incoming
mail and all non-navigation events refresh the panes normally.
