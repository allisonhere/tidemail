package autodiscover

import (
	"context"
	"errors"
	"net"
	"testing"
)

type fakeResolver struct {
	srv   map[string][]*net.SRV // "service.domain"
	mx    map[string][]*net.MX
	mxErr error
}

func (f fakeResolver) LookupSRV(_ context.Context, service, proto, name string) (string, []*net.SRV, error) {
	if recs, ok := f.srv[service+"."+name]; ok {
		return "", recs, nil
	}
	return "", nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (f fakeResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	if f.mxErr != nil {
		return nil, f.mxErr
	}
	if recs, ok := f.mx[name]; ok {
		return recs, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

// noFetch is a domain that publishes no autoconfig file.
func noFetch(context.Context, string) ([]byte, error) { return nil, errors.New("404 Not Found") }

func srv(target string, port uint16) []*net.SRV { return []*net.SRV{{Target: target, Port: port}} }
func mx(host string) []*net.MX                  { return []*net.MX{{Host: host, Pref: 10}} }

func TestDiscover(t *testing.T) {
	cases := []struct {
		name  string
		email string
		r     fakeResolver
		want  Result
		err   error
	}{
		{"srv with starttls submission", "alice@example.org", fakeResolver{srv: map[string][]*net.SRV{
			"imaps.example.org": srv("Mail.Example.org.", 993), "submission.example.org": srv("smtp.example.org.", 587),
		}}, Result{Servers: Servers{"mail.example.org", 993, true, "smtp.example.org", 587, true}, Source: "DNS SRV"}, nil},
		{"implicit-tls submissions preferred", "alice@example.org", fakeResolver{srv: map[string][]*net.SRV{
			"imaps.example.org": srv("mail.example.org.", 993), "submissions.example.org": srv("smtps.example.org.", 465),
			"submission.example.org": srv("smtp.example.org.", 587),
		}}, Result{Servers: Servers{"mail.example.org", 993, true, "smtps.example.org", 465, true}, Source: "DNS SRV"}, nil},
		{"submissions off 465 falls back to submission", "alice@example.org", fakeResolver{srv: map[string][]*net.SRV{
			"imaps.example.org": srv("mail.example.org.", 993), "submissions.example.org": srv("smtps.example.org.", 4650),
			"submission.example.org": srv("smtp.example.org.", 587),
		}}, Result{Servers: Servers{"mail.example.org", 993, true, "smtp.example.org", 587, true}, Source: "DNS SRV"}, nil},
		{"plaintext imap is never used", "alice@example.org", fakeResolver{srv: map[string][]*net.SRV{
			"imap.example.org": srv("mail.example.org.", 143), "submission.example.org": srv("smtp.example.org.", 587),
		}}, Result{}, ErrNotFound},
		{"dot target means not offered", "alice@example.org", fakeResolver{srv: map[string][]*net.SRV{
			"imaps.example.org": srv(".", 0), "submission.example.org": srv("smtp.example.org.", 587),
		}}, Result{}, ErrNotFound},
		{"imap without smtp falls through to mx", "alice@corp.example", fakeResolver{
			srv: map[string][]*net.SRV{"imaps.corp.example": srv("mail.corp.example.", 993)},
			mx:  map[string][]*net.MX{"corp.example": mx("ASPMX.L.GOOGLE.COM.")},
		}, Result{Servers: Gmail, Preset: "Gmail", Source: "MX records"}, nil},
		{"microsoft 365", "bob@contoso.example", fakeResolver{mx: map[string][]*net.MX{
			"contoso.example": mx("contoso-example.mail.protection.outlook.com."),
		}}, Result{Servers: Outlook, Preset: "Outlook", Source: "MX records"}, nil},
		{"icloud custom domain", "c@me.example", fakeResolver{mx: map[string][]*net.MX{"me.example": mx("mx01.mail.icloud.com.")}},
			Result{Servers: ICloud, Preset: "iCloud", Source: "MX records"}, nil},
		{"yahoo", "d@y.example", fakeResolver{mx: map[string][]*net.MX{"y.example": mx("mta5.am0.yahoodns.net.")}},
			Result{Servers: Yahoo, Preset: "Yahoo", Source: "MX records"}, nil},
		{"lookalike mx is not google", "e@x.example", fakeResolver{mx: map[string][]*net.MX{"x.example": mx("mx.evil-google.com.")}},
			Result{}, ErrNotFound},
		{"unknown mx", "f@x.example", fakeResolver{mx: map[string][]*net.MX{"x.example": mx("mx.x.example.")}}, Result{}, ErrNotFound},
		{"no records at all", "g@nothing.example", fakeResolver{}, Result{}, ErrNotFound},
		{"display name form", "Alice <alice@example.org>", fakeResolver{srv: map[string][]*net.SRV{
			"imaps.example.org": srv("mail.example.org.", 993), "submission.example.org": srv("smtp.example.org.", 587),
		}}, Result{Servers: Servers{"mail.example.org", 993, true, "smtp.example.org", 587, true}, Source: "DNS SRV"}, nil},
		{"bare login", "alice", fakeResolver{}, Result{}, ErrNoDomain},
		{"single-label domain", "alice@localhost", fakeResolver{}, Result{}, ErrNoDomain},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := discover(context.Background(), c.r, noFetch, c.email)
			if !errors.Is(err, c.err) || got != c.want {
				t.Fatalf("got %+v, %v; want %+v, %v", got, err, c.want, c.err)
			}
		})
	}
}

func TestDiscoverSurfacesResolverFailure(t *testing.T) {
	boom := &net.DNSError{Err: "server misbehaving", Name: "x.example", IsTemporary: true}
	_, err := discover(context.Background(), fakeResolver{mxErr: boom}, noFetch, "a@x.example")
	if err == nil || errors.Is(err, ErrNotFound) || !errors.Is(err, boom) {
		t.Fatalf("resolver failure reported as %v", err)
	}
}

func TestDiscoverStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := discover(ctx, fakeResolver{}, noFetch, "a@x.example"); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}
