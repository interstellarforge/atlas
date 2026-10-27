# ADR-0012: Optional outgoing email

- Status: accepted
- Date: 2026-10-27
- Supersedes: decision 2 of ADR-0009 ("No email"), partly: without configuration nothing changes.

## Context

SET 10 asks for an email provider so Atlas can mail invitations and notifications. ADR-0009 chose no email. Email must stay optional, must never break a mutation, and the SMTP password must not leak.

## Decisions

1. **SMTP only, standard library.** `net/smtp`, TLS (implicit), STARTTLS or none (plain auth is refused by the library on unencrypted non-local connections). No dependency. API providers can come later behind the same `Sender` interface.
2. **Settings:** environment defaults `ATLAS_SMTP_HOST/PORT/SECURITY/USER/PASSWORD/FROM/FROM_NAME` and `ATLAS_PUBLIC_URL` (for links). A row in `mail_settings`, saved by a global admin in a human session (`/admin/mail`), replaces the environment settings as a whole; "use the environment settings" deletes the row. Without either, no mail is queued or sent.
3. **The stored password is encrypted** (AES-256-GCM, random nonce) with a key in `<data dir>/mail.key` (32 random bytes, mode 0600, created on first save). It is never returned by the API (`password_set` only), never in the audit entry (`password_changed` only), never logged; send errors are redacted before they are stored, logged or shown. Trade-off: the key is not in the database or in a backup (backups hold the database and uploads), so a restore on another machine yields settings without a password: the admin enters it again. The key file guards against a leaked database file, not against a leaked data directory.
4. **Transactional outbox.** A mutation that causes a mail inserts a `mail_outbox` row in its own transaction (so a rolled-back mutation sends nothing). A worker (`internal/notifications`) polls every 5 s, sends outside any transaction, retries 5 times with 1, 5, 15 and 60 minute waits, then gives up. Failure of the mail server therefore never fails or delays a mutation, and a restart loses nothing. Sent and abandoned rows are blanked (the body can hold an invite link) and deleted after 14 days.
5. **First uses:** an invite with an email address is mailed next to the link; a notification is mailed to a user who switched on "email notifications" in their preferences (`email_notifications`, default off) and has not muted that type. Email-change confirmation is a separate story.
6. **Mail texts are templates** embedded under `internal/notifications/templates/<lang>/`; only `en` exists, other languages fall back to it.
7. **Test mail** goes straight through the sender to the admin's own address, with the error shown to the admin (redacted).

## Consequences

- Operators who never configure SMTP see no change; the invite link flow still works.
- Mail is at-least-once only in the sense of retries after an error that the server reported; a crash between the SMTP `DATA` end and the database update can send a mail twice.
- No bounce handling, no per-recipient rate limit, no HTML mail.
