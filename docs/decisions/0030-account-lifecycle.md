# ADR-0030: Account lifecycle, approval and activation policy

- Status: accepted, amended by ADR-0031 (account approval, `approval_by`, registration mode `request` and the pending/rejected states are retired in COM 2; the status gate, suspension, compare-and-set, non-disclosure and the no-access-by-registration rule stay)
- Date: 2026-11-10
- Tickets: ATL-492, ATL-366, ATL-361, ATL-362

## Context

Accounts are created by registration, invitation, an administrator or the organization migration. Only `deactivated_at` exists as state. Organizations and roles (ADR-0027, ADR-0029) need accounts that wait for a decision without gaining access.

## Decision

1. **Status.** `users.status` is `pending`, `active`, `rejected` or `suspended` (migration 0031; `deactivated_at` becomes `suspended`). Every authorization and authentication decision requires `active` (fails closed, ADR-0029 rule 5).
2. **Registration modes** (`registration` setting): `closed`, `invite`, `request`, `open`.
   - `closed`: no public form; a SUPERADMIN creates ACTIVE accounts.
   - `invite`: a code is needed. A SUPERADMIN registration invite yields ACTIVE; an organization invite yields PENDING.
   - `request`: self-registration yields PENDING ("awaiting approval").
   - `open`: ACTIVE; approval is deliberately skipped and not offered before the release gate (ATL-447).
3. **Transitions** (compare-and-set, one transaction with audit and change_event):

   | From | To | Who |
   |---|---|---|
   | pending | active, rejected | approver (rule 4) |
   | rejected | pending | the visitor, re-apply: 24 h after the rejection, at most 3 per 30 days |
   | rejected | active | approver |
   | active | suspended | SUPERADMIN |
   | suspended | active | SUPERADMIN |

   Suspension and rejection delete sessions, revoke tokens and OAuth grants and close SSE streams in the same transaction. The last-SUPERADMIN guard (ATL-483) applies.
4. **Who approves is a setting.** `approval_by` in `instance_settings`: `superadmin` (default) or `admin`. With `admin`, an ADMIN of any organization may also approve or reject PENDING accounts; suspend and reactivate stay SUPERADMIN-only. Why any organization and not "their own": a PENDING account has no organization yet. The setting changes who decides, never what approval grants.
5. **Approval grants no access.** No organization or project access follows from approval. Memberships from an organization invitation stay inert until ACTIVE and take effect at that moment. Organization ADMINs cannot create or activate accounts, only invite.
6. **Human decision only.** Approve, reject, suspend and reactivate need a human session and CSRF; tokens, OAuth and MCP get 403.
7. **No enumeration.** Register with an existing email answers like a new one; invitation responses are identical whether or not the email has an account. Wrong credentials give a generic 401; correct credentials of a non-active account give 403 with the state code; session and token paths give 401.

## Consequences

- One status check replaces the ad-hoc `deactivated_at` checks (ATL-368); `deactivated_at` stays until the anonymise and trigger paths move to `status`.
- Email normalization (trim, lowercase) is required before the uniqueness check.
- Docs to update after REG 1-3: `data-model.md`, `api-contract.md`, `security.md`, README policy section.
