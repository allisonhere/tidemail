# Google OAuth registration and release setup

This setup is for TideMail maintainers. Users of configured release builds only
choose **Sign in with Google** and approve access.

## Initial testing

Use the existing Google project for initial development:

1. In Google Cloud Console, select the project and enable the Gmail API.
2. Configure Google Auth Platform branding and an External audience. Keep the
   project in Testing and add the intended Gmail address as a test user.
3. Under Clients, create or select a **Desktop app** client. A Web or TV client
   is not appropriate for this flow.
4. Configure the requested data-access scope, `https://mail.google.com/`, which
   TideMail uses for IMAP and SMTP.
5. Supply the Desktop client's ID and secret through the environment before
   starting a development build:

   ```sh
   export TIDEMAIL_GOOGLE_CLIENT_ID='your-desktop-client-id.apps.googleusercontent.com'
   export TIDEMAIL_GOOGLE_CLIENT_SECRET='your-desktop-client-secret'
   go run .
   ```

These exports now override saved settings, including empty credentials left by
older versions. No config-file edit is needed. Both values must belong to the
same client. Nonempty environment values take precedence over saved custom
credentials; otherwise the app uses bundled defaults. A different client ID
does not inherit another client's secret.

Use `M` → Gmail → OAuth → `Ctrl+O`, approve in the browser, then `Ctrl+S`.
Verify inbox loading, send a test message to yourself, and restart TideMail to
confirm the saved refresh token works. In Testing, Google generally expires
these Gmail refresh tokens after seven days.

## Release credentials

Configure repository Actions secrets named `TIDEMAIL_GOOGLE_CLIENT_ID` and
`TIDEMAIL_GOOGLE_CLIENT_SECRET` with the production Desktop client's values.
The release workflow passes them to `scripts/build-release.sh`, which requires
both values and embeds them using Go linker flags. To build locally with
credentials already in your environment:

```sh
bash scripts/build-release.sh v0.0.0-test ./tidemail
```

The script respects `GOOS` and `GOARCH` and sets `CGO_ENABLED=0`. It rejects
missing credentials before building. CI's ordinary `go build` and `go test`
remain usable without real credentials.

Desktop OAuth clients are public clients. The bundled secret is extractable
from the executable, so it is not a security boundary. Never use a Web client's
confidential credentials here. User tokens remain on the user's machine using
TideMail's existing keychain/config fallback; there is no TideMail OAuth relay.

Bundled defaults are not saved as custom overrides. Existing custom client
settings remain supported. Refresh tokens belong to their issuing client:
switching a test installation to the production registration requires signing
in again. Do not rotate the production client ID casually.

## Public rollout

Prepare a separate production project with TideMail branding, verified domain,
homepage, privacy policy, support contact, scope justification, and a video of
the actual consent and mail-access flow. Submit for Google's restricted-scope
verification before general availability. Follow Google's determination of
any required security assessment for TideMail's actual data flows.

Testing allows up to 100 designated test users. Publishing an unverified Gmail
app does not remove the separate 100-user cap on unverified access. Verification
of the requested Gmail scope is needed to lift that cap. Merely embedding
credentials, passing tests, or marking the app Production does not establish
verification; maintainers must confirm approval before public release.

References:

- [Google Desktop OAuth and PKCE](https://developers.google.com/identity/protocols/oauth2/native-app)
- [Desktop loopback callbacks](https://developers.google.com/identity/protocols/oauth2/resources/loopback-migration)
- [Gmail XOAUTH2](https://developers.google.com/workspace/gmail/imap/xoauth2-protocol)
- [Audience and user limits](https://support.google.com/cloud/answer/15549945)
- [Restricted-scope verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/restricted-scope-verification)
