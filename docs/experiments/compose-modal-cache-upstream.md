# Compose modal caching on upstream main

## Provenance and scope

Base: Allie's upstream `main`, `b50eabf7a12c110d51cd79e5a7179d244ca51754`
(v1.9.0). Branch: `perf/compose-modal-cache`; worktree:
`/home/chris/Development/tidemail-compose-cache`. Local main was safely advanced
from `30009d3902f3bce8b92bc43524d465061c9b5053` to this upstream commit.

The accepted experiment and its historical report remain untouched in
`/home/chris/Development/tidemail`. This port retains pane, status/background,
shadow/composition and final-row caches, bounded scalar dependency keys, display
revisions at mutation/result boundaries, and relative-label expiry deadlines.
Cache hits do not traverse message collections or serialize mail content.

Upstream's newer mouse geometry, per-motion autoscroll, word-wrap setting,
Ripple version, input decoder, status pills, transparency and destructive-action
watchdog remain intact. Necessary adaptations are a revision increment for the
new watchdog result, preserving upstream's final transparent-background pass,
and test geometry signatures. No pointer-receiver optimization was imported.

Production files changed: `internal/ui/{compose.go,compose_backdrop_cache.go,
model.go,content.go,messages.go,threading.go,outbox.go,sidebar.go,sync.go,
overlays.go,modal_cache.go}`. Tests: `compose_backdrop_test.go`,
`compose_cache_revision_test.go`, `modal_cache_test.go`. The mutation hooks
invalidate displayed panes; status changes use their separate bounded key.
Fresh-versus-cached tests cover asynchronous results, message/read state,
selection/thread/draft changes, reader changes, resize/theme/transparency,
status/sync and relative-time boundaries. Documentation includes this record and
[the separate reuse assessment](modal-cache-reuse-assessment.md).
No help/keybinding change is needed: controls and intended visual behavior are
unchanged.

## Focused validation

Focused cache, mouse, wrapping and transparency tests passed (1.990 s).
Focused race checks including new upstream delete/cursor/search/status behavior
passed (12.386 s). `go vet ./...` passed; golangci-lint v2.15.0 reported zero
issues. These checks were not followed by redundant full-suite runs.

`BenchmarkComposeRevisionKeyScaling` measured 310.1–350.6 ns/op, 3 B/op,
1 allocation/op across 20, 200 and 1,000 messages with 1 KB and 32 KB bodies.
This preserves the original correction's bounded cost; the original expensive
fingerprint measurements remain historical evidence in the untouched experiment.

## Controlled full Foot/PTY comparison

The full report, raw traces, disposable source copies and scripts are outside
production worktrees at
`/home/chris/Development/tidemail-cache-port-artifacts/`.
Read `A-B-comparison.md` there for every requested timing, allocation, decode and
selection metric, exact conditions and methodology limitations.

Both full applications used the exact base above, identical upstream mouse and
Ripple code, synthetic 1,000-message fixtures, Foot 160×50, and two-second timed
SGR replay. Twelve paired runs covered 1 KB and 27,200-byte drafts at 60/120 Hz,
edge autoscroll and maximized control. Each run used a fresh fixture copy.

| Workload | Upstream / cache median View ms | Upstream / cache input-to-View p95 ms | Upstream / cache release backlog ms |
|---|---:|---:|---:|
| Large compact 60 Hz | 10.087 / 3.018 | 17.781 / 9.920 | 0.261 / 0.234 |
| Large compact 120 Hz | 6.461 / 2.764 | 528.523 / 9.818 | 522.950 / 0.411 |
| Short compact 60 Hz | 9.532 / 1.960 | 16.459 / 7.177 | 1.194 / 0.215 |
| Short compact 120 Hz | 6.090 / 2.094 | 313.516 / 7.719 | 293.066 / 0.117 |
| Edge compact 120 Hz | 6.300 / 2.699 | 541.099 / 9.653 | 529.070 / 0.168 |
| Maximized 120 Hz | 2.977 / 2.468 | 11.959 / 8.388 | 0.125 / 0.164 |

Upstream sustained 60 Hz and the maximized control. In compact 120 Hz cases it
fell behind and malformed input fragments changed the synthetic drafts, so its
aggregate Update/View durations there are not clean like-for-like costs after
corruption. The cache decoded all expected packets, preserved drafts and
selection, processed release promptly, and performed the expected edge
scrolling. This does not establish a universal decoder fix. Physical display
presentation latency was not measured; output measurements stop at submission.

At healthy large-draft 60 Hz, median Update was 4.108 / 4.477 ms and p95
5.654 / 6.267 ms; View p95 was 12.143 / 3.505 ms. Input-to-output submission
p95 was 33.642 / 16.970 ms. Allocated bytes per decoded event fell from
9,870,114 to 4,086,701. Mouse geometry still renders the editor in upstream's
Update path: that unchanged cost remains, and is not an additional optimization
in this change. Maximized differences should be treated as run variability,
since the compact backdrop caches are bypassed there.

## Manual artifact and stopping point

Complete uninstrumented build:

```
GOTMPDIR=/home/chris/Development/tidemail-cache-port-artifacts/buildtmp go build -trimpath -buildvcs=false -ldflags='-s -w' -o /home/chris/Development/tidemail-cache-port-artifacts/tidemail-compose-modal-cache-main .
foot /tmp/tidemail-compose-modal-cache-main-v1.9.0
```

The new `/tmp` launch path is a symlink to that full executable because `/tmp`
data quota is exhausted. It does not replace any installed or earlier binary.
The accepted experiment already passed manual testing; this upstream port awaits
final user comparison. Automated stopping conditions have been met. No further
profiling, commits, pushes or PR creation were performed.
