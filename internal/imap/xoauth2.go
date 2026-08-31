package imap

import (
	"fmt"

	"github.com/emersion/go-sasl"
)

// xoauth2Client is a sasl.Client for the XOAUTH2 mechanism (Gmail's IMAP OAuth;
// go-sasl ships OAUTHBEARER but not XOAUTH2). go-imap handles the base64
// framing, so Start returns the raw initial response.
type xoauth2Client struct {
	user, token string
}

var _ sasl.Client = (*xoauth2Client)(nil)

func (c *xoauth2Client) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + c.user + "\x01auth=Bearer " + c.token + "\x01\x01"), nil
}

func (c *xoauth2Client) Next(challenge []byte) ([]byte, error) {
	// On failure the server challenges with a JSON error blob; per the XOAUTH2
	// spec the client answers with an empty response to receive the final NO.
	return nil, fmt.Errorf("xoauth2 rejected: %s", challenge)
}
