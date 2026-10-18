# Template plugins (design only, not built)

Status: proposal. Nothing here is implemented.

TideMail has core templates: built-ins plus `.md`/`.txt` files in the
`templates/` folder next to `config.toml`, inserted from compose with F6.

## What plugins can add

Plugins cannot add UI or change mail ([README](../plugins/README.md)), so a
plugin-drawn template editor is out. Two things fit the model:

1. **Template packs need no plugin.** A pack is a folder of template files.
   Sharing one means copying it into `templates/`. If a catalog entry is wanted
   later, it can be a data-only package with no executable.
2. **A `template.render` method.** For templates whose text depends on live
   data (free meeting slots, an order status, a CRM snippet), a plugin can
   return plain text for a named template.

## `template.render` sketch

- Permission: `template_render`, declared in the manifest and shown at install
  time like any other permission.
- Request: the template name, the To address and subject, and the sending
  account's name. No message bodies.
- Response: `{"text": "..."}` and an optional `subject`. Plain text only; unknown
  fields are errors, like views.
- TideMail inserts the text at the cursor as one undo step, exactly like a file
  template. The user reads and edits it before sending. The plugin cannot send,
  and nothing runs automatically.
- Needs a protocol version bump and a `compatibility.md` entry.

## Rejected

- Plugin-drawn editor or panels: breaks "Plugins provide semantics. TideMail
  controls presentation."
- Plugins that insert or send without the user choosing the template.

## Build only when

A real plugin needs live data in a template. Until then the folder is enough.
