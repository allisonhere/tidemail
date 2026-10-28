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

The initial pane-reuse run removed growing backlog at 30 Hz: final-view drain
was 11–22 ms. Combined p95 cycles were approximately 15–24 ms, so the 20 ms
target was not met in every case. Wide 120 Hz wheel stress still accumulated
264–389 ms of backlog; narrow 80×24 stress drained in 11 ms. These are local
diagnostic observations, not cross-machine timing guarantees.

Discarded output does not measure terminal painting or physical trackpad
momentum. Interactive terminal replay and manual mouse/trackpad testing are
separate checks. Keep the three-row wheel increment unchanged while evaluating
the rendering improvement.
