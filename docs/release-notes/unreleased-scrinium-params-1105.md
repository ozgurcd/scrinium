# Release-notes fragment: parameter refusals (P-20)

Patch release; fold into the next version's release notes.

- A tool that takes no parameters now names the parameters it refused (sorted, at most five, each cut to 64 bytes) instead of only "does not accept parameters". `session_begin` and `begin_session` add that a session takes no title: keep the `session_id` it returns (P-20). Accepted parameters and results are unchanged.
