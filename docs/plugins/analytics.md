# Aggregate analytics

The `analytics.*` report queries return **computed statistics, not message
records**. They need only the `analytics_read` permission and are the safest
way to build dashboards, digests, and reports.

> Aggregate analytics permissions disclose computed statistics without
> necessarily disclosing individual message records.

How to run queries at all (the `report.run` capability, rounds, limits,
errors) is in [queries.md](queries.md).

## What gets counted

- Every cached folder except Trash, Junk, and Drafts.
- Each message **once**, however many folders hold a copy (for example a
  Gmail message in both Inbox and All Mail).
- A message is **sent** when it is from one of your account addresses or is
  in a Sent folder, and **received** otherwise.
- Only what TideMail has cached: folders that have not been synced are not
  counted.

## Ranges

Analytics that cover a period take either a `range`:

```text
7d  30d  90d  365d  all
```

(default `30d`, ending at the report's `now`), or an explicit `from` and `to`
(RFC 3339, `[from, to)`). Natural-language dates are not accepted.

## `analytics.volume`

Received and sent counts per period.

```json
{"method": "analytics.volume", "range": "30d", "group_by": "day"}
```

```json
{
  "group_by": "day",
  "from": "2026-08-28T12:00:00-05:00",
  "to": "2026-09-27T12:00:00-05:00",
  "received": 428,
  "sent": 73,
  "buckets": [{"start": "2026-08-28", "received": 12, "sent": 3}]
}
```

`group_by` is `day` (default), `week` (ISO weeks, starting Monday), or
`month`, in the report's timezone. Every bucket in the range is present, even
empty ones. A range with more than 2000 buckets fails; group by week or month.

## `analytics.categories`

Received mail by **effective** category: plugin `category` annotations with
the user's corrections applied, the same category TideMail shows. A
correction wins over any plugin; uncategorized mail is `none`.

```json
{"method": "analytics.categories", "range": "30d"}
```

```json
{"total": 999, "categories": [{"category": "newsletter", "count": 471}, {"category": "github", "count": 298}, {"category": "none", "count": 87}]}
```

## `analytics.attention`

TideMail's attention views, **as they are now**.

```json
{"method": "analytics.attention"}
```

```json
{
  "needs_you_current": 12,
  "needs_you_dismissed_current": 3,
  "waiting_current": 5,
  "snoozed_messages_current": 2,
  "snoozed_threads_current": 1,
  "corrections_current": 9
}
```

Counts over time are not offered because TideMail does not keep that history:
a snooze is deleted when it ends, and a correction keeps only its latest
value. Rather than estimate history from current state, this query reports
current state only. It takes no parameters.

## `analytics.response_times`

How long replies take, in seconds.

```json
{"method": "analytics.response_times", "range": "90d"}
```

```json
{
  "user_sample_count": 41,
  "median_user_response_s": 5400,
  "average_user_response_s": 20160,
  "other_sample_count": 37,
  "median_other_response_s": 9000,
  "average_other_response_s": 33300,
  "excluded_count": 0
}
```

- A **user response** is the time from someone else's message (the first of
  an unanswered run) to your next message in the same conversation; an
  **other response** is the reverse. Samples count when the reply is in the
  range.
- It uses TideMail's Message-ID threading. It is **an approximation**:
  replies whose other half is not cached (an unsynced Sent folder, say) are
  missed, and clients that drop `References` split conversations. Robots and
  mailing lists do not count as the other side.
- Without your account addresses (no accounts configured) every count is 0.

## `analytics.contacts`

Your most frequent correspondents and domains.

```json
{"method": "analytics.contacts", "range": "365d", "limit": 10}
```

```json
{
  "correspondents": [{"address": "ann@example.com", "name": "Ann Lee", "received": 40, "sent": 22, "total": 62}],
  "domains": [{"domain": "example.com", "received": 90, "sent": 31, "total": 121}]
}
```

- Addresses are normalized (lowercase, display names removed); `name` is the
  most recent display name seen.
- **received** counts messages from that address; **sent** counts your
  messages to it (To and Cc). Your own addresses never appear.
- `limit` is 1 to 50 (default 10) and applies to each list.
