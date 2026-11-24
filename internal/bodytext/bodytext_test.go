package bodytext

import "testing"

func TestResolve(t *testing.T) {
	for _, tc := range []struct{ plain, html, want, source string }{
		{" Original\n text ", "<p>alternative</p>", " Original\n text ", "plain"},
		{" \t\u00a0", "<head><title>secret</title><style>secret</style></head><p>Hello &amp; <a href='https://tracking.invalid/token'>world</a></p><script>secret</script><p>Next<br>line</p>", "Hello & world\nNext\nline", "html"},
		{"", "<table><tr><td>one</td><td>two</td></tr><tr><td>three</td></tr></table>", "one two\nthree", "html"},
		{"", "<p>Before <b>bold</b></p><div hidden>secret</div><template>secret</template><p>After", "Before bold\nAfter", "html"},
		{"", "<img src='https://tracking.invalid/image'>", "", "none"},
		{"", "<dl><dt>Order</dt><dd>12345</dd></dl><hr><footer>Footer</footer>", "Order\n12345\nFooter", "html"},
	} {
		got, source := Resolve(tc.plain, tc.html)
		if got != tc.want || source != tc.source {
			t.Fatalf("got %q (%s), want %q (%s)", got, source, tc.want, tc.source)
		}
	}
}
