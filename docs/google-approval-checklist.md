# Google approval checklist

TideMail is a desktop email client, an approved Gmail use case. Its current
IMAP and SMTP implementation requests `https://mail.google.com/`, however,
which is a restricted scope. Approval requires Google OAuth verification and
an annual security assessment at the tier Google assigns.

## TideMail engineering work

- [ ] Add an in-app disclosure immediately before Google authorization. It
  must say that TideMail accesses mail to show, compose, send, organize, and
  delete it, and that the user must actively choose **Continue** before the
  browser opens.
- [ ] Explain external data transfers at that same point. In particular,
  optional AI features must state which selected provider receives mail content
  and require the user's affirmative action before any transfer.
- [ ] Ensure Google user data is never used to train or improve a general AI
  model, and document the behavior of every supported AI provider.
- [ ] Provide a visible way to remove a Gmail account and its local cache,
  refresh token, drafts, and attachments. Add user documentation for that
  deletion path.
- [ ] Keep all Google access on the user’s device, use TLS for IMAP/SMTP and
  OAuth, and keep refresh tokens in the existing OS keychain/config fallback.
- [ ] Keep the OAuth request limited to `https://mail.google.com/`; do not add
  scopes without a separately reviewed feature and verification update.
- [ ] Make permanent deletion a deliberate, documented user action and include
  it in the verification video. Google allows this IMAP/SMTP scope only when
  the app needs immediate permanent deletion that bypasses Trash. TideMail
  currently performs IMAP EXPUNGE, so this behavior needs a clear product-level
  explanation and demonstration.
- [ ] Publish a TideMail homepage, privacy policy, data-deletion help page,
  support contact, and a Limited Use statement. The policy must accurately
  describe local storage, optional AI transfers, retention, and deletion.
- [ ] Prepare a release build from the production Google Desktop client and
  confirm Gmail sign-in, read, send, organize, permanent delete, account
  removal, and re-authentication after restart.
- [ ] Record an unlisted English-language verification video that shows the
  disclosure, OAuth consent screen, all requested scope use, and data deletion.

## Maintainer actions

- [ ] Rotate the Google Desktop client secret that was shared during setup;
  update both the local test environment and the two GitHub Actions secrets.
- [ ] Create a separate Google Cloud production project. Keep the existing
  project for development/testing so its test-user history and experimental
  clients do not complicate production review.
- [ ] In the production project, enable Gmail API and create one Desktop app
  OAuth client for TideMail.
- [ ] Configure Google Auth Platform branding: TideMail name, logo, support
  email, developer contacts, External audience, homepage, privacy-policy URL,
  and authorized domain.
- [ ] Verify ownership of every authorized domain in Google Search Console from
  a Google account that owns or edits the Cloud project.
- [ ] Add only `https://mail.google.com/` to Data Access. Write a scope
  justification that describes TideMail as a desktop client and explains why
  its immediate permanent-delete feature requires IMAP/SMTP rather than a
  narrower Gmail API scope.
- [ ] Remove incomplete or unused OAuth clients from the production project
  before submitting. Every restricted-scope client in that project must be
  ready for review.
- [ ] Put the production client ID and secret in GitHub repository Actions
  secrets named `TIDEMAIL_GOOGLE_CLIENT_ID` and
  `TIDEMAIL_GOOGLE_CLIENT_SECRET`; release builds already consume them.
- [ ] Submit the production project through Google Cloud Console’s OAuth
  consent-screen verification flow. Provide the homepage, privacy policy,
  scope justification, support contact, and the unlisted demo-video link.
- [ ] Complete the CASA security assessment Google assigns. Plan for annual
  reassessment and prompt responses to Google’s verification emails.
- [ ] Do not publish a general release or invite more users until the restricted
  scope is verified. Unverified restricted-scope projects are subject to a
  lifetime cap of 100 new users.

## Google references

- [Gmail IMAP/SMTP XOAUTH2 scope](https://developers.google.com/workspace/gmail/imap/xoauth2-protocol)
- [IMAP/SMTP minimum-scope rule and verification FAQ](https://support.google.com/cloud/answer/13463817)
- [Brand, scope, video, and security-assessment requirements](https://support.google.com/cloud/answer/13464321)
- [Google Workspace user-data and Limited Use policy](https://developers.google.com/workspace/workspace-api-user-data-developer-policy)
