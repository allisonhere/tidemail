package plugin

import (
	"reflect"
	"testing"
)

func TestAuthFromHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers string
		want    *MessageAuth
	}{
		{"gmail style", "Authentication-Results\nmx.google.com; dkim=pass header.i=@example.com header.s=s1; spf=pass (google.com: domain of a@example.com designates 1.2.3.4 as permitted sender) smtp.mailfrom=a@example.com; dmarc=pass (p=NONE sp=NONE dis=NONE) header.from=example.com\n",
			&MessageAuth{SPF: "pass", DKIM: "pass", DMARC: "pass"}},
		{"failures", "Authentication-Results\nmx.example.net; spf=softfail smtp.mailfrom=x@evil.test; dkim=fail; dmarc=fail (p=REJECT)\n",
			&MessageAuth{SPF: "softfail", DKIM: "fail", DMARC: "fail"}},
		{"none reported for a mechanism", "Authentication-Results\nmx.example.net; spf=pass smtp.mailfrom=a@b.test\n",
			&MessageAuth{SPF: "pass"}},
		{"received-spf only", "Received-SPF\nPass (mailfrom) identity=mailfrom; client-ip=1.2.3.4\n",
			&MessageAuth{SPF: "pass"}},
		{"authentication-results wins over received-spf", "Authentication-Results\nmx; spf=fail\nReceived-SPF\npass (x)\n",
			&MessageAuth{SPF: "fail"}},
		{"a passing own signature beats an earlier failing third-party one", "Authentication-Results\nmx; dkim=fail header.d=list.example; dkim=pass header.d=example.com\n",
			&MessageAuth{DKIM: "pass"}},
		{"unknown values are 'other'", "Authentication-Results\nmx; spf=whatever; dmarc=bestguesspass\n",
			&MessageAuth{SPF: "other", DMARC: "other"}},
		{"case is ignored", "Authentication-Results\nMX; SPF=PASS; DKIM=Pass\n",
			&MessageAuth{SPF: "pass", DKIM: "pass"}},
		{"provider spam flag", "X-Spam-Status\nYes, score=9.1 required=5.0\n", &MessageAuth{SpamFlag: true}},
		{"provider says no spam", "X-Spam-Status\nNo, score=-1.0\n", nil},
		{"other cached headers alone", "Return-Path\n<a@b.test>\nReceived\nfrom x by y\nList-Unsubscribe\n<https://x>\n", nil},
		{"empty", "", nil},
		{"a value that merely looks like a label is not read as one", "Subject\nAuthentication-Results\n", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := AuthFromHeaders(tc.headers); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
