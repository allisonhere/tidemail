# tidemail-pgp

OpenPGP for TideMail, using your own GnuPG. It decrypts and verifies received
mail (`message.decode`, permission `message_raw`) and signs and encrypts
outgoing mail (`message.prepare`, permission `message_compose`).

TideMail does the MIME work and hands this plugin only the bytes to check or
protect; the plugin runs `gpg` against your keyring and returns the result. No
key leaves GnuPG.

## Install

Needs `gpg` (GnuPG 2.x) on your `PATH`, your secret key, and the public keys of
the people you write to.

```sh
go build -o tidemail-pgp ./examples/plugins/pgp
mkdir -p ~/.config/tidemail/plugins/pgp
cp tidemail-pgp examples/plugins/pgp/plugin.toml ~/.config/tidemail/plugins/pgp/
```

Restart TideMail. In a message, press `D` to decrypt and verify; in compose,
`F8` signs and `F9` encrypts. The user guide has the details and limits:
[PGP encryption and signing](../../../docs/guide.md#pgp-encryption-and-signing).

## Settings

- **Key trust** (`gpg` by default): `gpg` follows GnuPG's trust rules, so
  recipients' keys must be signed or trusted. `always` encrypts to any key in
  your keyring.

## Passphrases

GnuPG asks for your passphrase through pinentry, which must be graphical or be
served by an agent that has the passphrase cached (a terminal pinentry cannot
draw over TideMail). The plugin has up to two minutes (`timeout` in
`plugin.toml`) for you to answer.

## What it does not do

No key management (use `gpg`), no key-server lookups (they are disabled so
nothing leaks about whom you write to), no attachments inside encrypted mail
on the reading side yet.

## Tests

`go test ./examples/plugins/pgp` generates throwaway keys in a temporary
`GNUPGHOME` and drives real `gpg`, including a full round trip through
TideMail's plugin runtime. The tests are skipped when `gpg` is missing.
