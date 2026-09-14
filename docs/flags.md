# Development flags

Run development flags after the package argument:

```sh
go run . --disable-google-oauth
```

| Flag | Purpose |
| --- | --- |
| `--disable-google-oauth` | Developer/test flag that additionally clears Gmail OAuth client credentials for this run. Gmail OAuth controls are already hidden while Google approval is pending. It does not rewrite `config.toml` or disable Outlook OAuth. |
| `--preview-manual-update` | Opens the app with the manual-update UI path available for testing. |
| `--preview-update-progress` | Previews the in-app update progress state. |
| `--prototype-forms` | Runs the standalone form prototype instead of the mail client. |

While Google approval is pending, a Gmail account form omits the Auth selector
and Google sign-in button. It shows this guidance under Password:

```text
Google OAuth is waiting for Google's approval.
Use a Google App Password instead.
```

The flag also protects an existing Gmail OAuth account from conversion to
password authentication if you save its form during a preview.
