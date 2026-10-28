package main

import "time"

const hour = time.Hour

func personalMessages() []seedMessage {
	return []seedMessage{
		{
			from: "Maya Chen <maya@example.org>", to: "alex@example.com", subject: "Saturday hike?", ago: 25 * time.Minute,
			text: "Hey Alex,\n\nThe forecast looks perfect for Saturday. Want to try the ridge trail? I can drive, leaving around 8.\n\nBring the good camera!\n\nMaya",
		},
		{
			from: "The Weekly Tide <news@tidenews.example>", to: "alex@example.com", subject: "This week in terminal software", ago: 3 * hour, starred: true,
			text: "This week in terminal software\n\nRead the full issue: https://tidenews.example/issue/42",
			html: `<html><body style="font-family:sans-serif"><h2>This week in terminal software</h2>
<p>Five tools worth your time, and one worth skipping.</p>
<p><a href="https://tidenews.example/issue/42?utm_source=mail&utm_campaign=weekly">Read the full issue</a></p>
<p><a href="https://tidenews.example/archive">Browse the archive</a></p>
<p><a href="https://track.tidenews.example/o?id=42"><img src="https://track.tidenews.example/p.gif" width="1" height="1" alt=""></a></p>
</body></html>`,
		},
		{
			from: "Parcel Express <noreply@parcel.example>", to: "alex@example.com", subject: "Your package is out for delivery", ago: 5 * hour,
			text: "Good news! Your package is out for delivery today. Track it: https://parcel.example/track?n=8841",
			html: `<html><body><p>Good news! Your package is out for delivery today.</p>
<p><a href="https://parcel.example/track?n=8841" style="background:#2b6cb0;color:#fff;padding:10px 18px;border-radius:4px">Track your package</a></p>
<p><a href="https://click.parcel.example/open?id=8841"><img src="https://click.parcel.example/pixel.gif" width="1" height="1"></a></p></body></html>`,
		},
		{
			from: "Sam Okafor <sam@example.net>", to: "alex@example.com", subject: "Re: Dinner on the 14th", ago: 9 * hour, read: true,
			text: "Works for me! 7pm at the usual place. I'll book the corner table.\n\n> Are you free on the 14th?\n> Thinking Italian.",
		},
		{
			from: "Lena Park <lena@example.com>", to: "alex@example.com", subject: "Photos from last weekend", ago: 26 * hour, read: true, attachment: true,
			text: "Here are the photos from the lake. The sunset ones came out great!",
		},
		{
			from: "Open Source Weekly <hello@osweekly.example>", to: "alex@example.com", subject: "Maintainers: how to get your first sponsor", ago: 50 * hour, read: true,
			text: "Funding open source is hard. Here is what worked for three small projects.\n\nhttps://osweekly.example/first-sponsor",
		},
	}
}

func workMessages() []seedMessage {
	return []seedMessage{
		{
			from: "Priya Nair <priya@acme.example>", to: "alex@acme.example", subject: "Q3 planning: your section", ago: 40 * time.Minute, starred: true,
			text: "Hi Alex,\n\nCould you fill in the infrastructure section of the Q3 plan by Thursday? Draft is here: https://docs.acme.example/q3-plan\n\nThanks!\nPriya",
		},
		{
			from: "Build Bot <ci@acme.example>", to: "alex@acme.example", subject: "[acme/api] Pipeline passed on main", ago: 2 * hour, read: true,
			text: "Pipeline #4821 passed on main in 6m 12s.\n\nhttps://ci.acme.example/pipelines/4821",
		},
		{
			from: "Jordan Lee <jordan@acme.example>", to: "alex@acme.example", subject: "Design review notes", ago: 7 * hour,
			text: "Notes from this morning:\n\n1. Ship the new onboarding behind a flag\n2. Revisit the empty states\n3. Accessibility pass before launch\n\nI'll send the full deck tomorrow.",
		},
		{
			from: "Finance <finance@acme.example>", to: "alex@acme.example", subject: "Expense report approved", ago: 30 * hour, read: true,
			text: "Your expense report for September has been approved and will be paid in the next cycle.",
		},
	}
}
