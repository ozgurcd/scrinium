# Release-notes fragment: --json refusal before any change (SCR-01)

Patch release; fold into the next version's release notes.

- A compatibility command given `--json` (for example `begin_session` or `update_wiki_page`) is now refused before it runs. It used to create the session or write the page and then fail with "--json is supported only by the public … operations", so a retry made a second change. The public commands' `--json` output is unchanged (SCR-01).
