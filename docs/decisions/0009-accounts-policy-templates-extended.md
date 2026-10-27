# ADR-0009: Account security, registration policy and wider templates

- Status: accepted
- Date: 2026-10-27

## Decisions

1. **Policy in the database, bootstrap in the environment.** Registration mode, allowed email domain, minimum password length and session length live in `instance_settings` and are edited by admins (hard floor 8 characters, enforced in code). Everything that was an `ATLAS_*` variable stays read-only. Session length is pushed into the auth service at start and after a change.
2. **No email.** Invites are links an admin hands over; email change is not confirmed by mail. Both are documented in the UI. If a mail server is ever added, confirmation and invite delivery are the first uses.
3. **TOTP, not WebAuthn.** Standard RFC 6238 with stdlib only (no dependency). The secret is stored readable because verification needs it; the risk and the replay protection are in `docs/security.md`. Recovery codes are hashed and single use; admins can reset.
4. **Deleting an account anonymises it.** Hard-deleting a user row would break audit and authorship references. The row stays with a placeholder name and an unusable email and password; memberships, sessions, tokens, follows and notifications are removed. The instance and projects are never left without an admin.
5. **Profile pictures reuse the media pipeline** and serve only a re-encoded thumbnail.
6. **Item templates** gain a subitem tree (3 levels, 50 items, hierarchy rules checked on save) created in the same transaction as the item, and an instance-wide scope (`project_id` NULL, admin managed, overridden by a project template of the same name).

## Consequences

- A lost second factor without recovery codes needs an admin reset.
- Subitems use the default status; if it blocks creating, the whole template create fails and nothing is left behind.
- Custom-field values and personal templates are still not part of item templates.
