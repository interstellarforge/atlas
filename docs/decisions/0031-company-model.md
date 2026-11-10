# ADR-0031: Company model: company-owned administration, free accounts, join flows and support access

- Status: accepted (owner decisions of 2026-11-10)
- Date: 2026-11-10
- Amends: ADR-0027 (points 2, 6, 8, 9, 10), ADR-0029 (SUPERADMIN holds all actions, `org.any`), ADR-0030 (account approval)
- Tickets: epic COM (see Atlas)

## Context

Today the SUPERADMIN approves every account and reaches every project. The intended model is Jira-like: a company gets its own space and its own ADMIN, who manages projects, settings, invitations, join requests and members. The Atlas admin runs Atlas itself, not the companies' work.

## Decision

1. **Words.** The name is "organization" everywhere (UI, API, code, tables). Where this document says "company" it means an organization.
2. **Roles.** SUPERADMIN = Atlas admin (instance only). Company ADMIN = runs one company. USER = member with explicit project membership. No custom roles in this change (epic ADV stays later).
3. **SUPERADMIN scope.** Instance settings, outgoing mail, backups, updates, server audit, companies (create, archive, set the first ADMIN), company applications, domain confirmation, account suspension. A SUPERADMIN has no access to projects, items, comments, attachments, project or member lists of any company. They see company name, key, status, ADMIN emails and member counts. `org.any` and the SUPERADMIN branch of project authorization are removed.
4. **Support access (company setting).** Off by default. The company ADMIN can allow it as `read_only` or `full`, time-limited (hours) or until revoked, and revoke it at any time. It is stored as a grant (`organization_id`, `level`, `granted_by`, `granted_at`, `expires_at`, `revoked_at`). Only while a valid grant exists may a SUPERADMIN session open that company; read-only gives `view` and `export`, full gives ADMIN actions. Every use is audited with the grant id. Authz reads the grant live.
5. **Accounts are free.** An account is a global identity. Registration (instance mode `closed`, `invite` or `open`) creates an ACTIVE account with no memberships and therefore no access. Account status is `active` or `suspended` (SUPERADMIN only; the ADR-0030 status gate, suspension revocation and compare-and-set are kept). `pending` and `rejected` stop being account states; the instance mode `request` and the `approval_by` setting are removed.
6. **Verified email.** `users.email_verified_at` is set when a person follows an emailed link (invitation, verification). Verified email is required for automatic domain join. Without outgoing mail configured nothing can auto-join.
7. **Creating a company.** (a) A SUPERADMIN creates it with a first ADMIN (existing account or invitation). (b) Company application: when the instance setting `company_creation` is `apply` (default `admin_only`), a signed-in account submits a name, a desired key and a note. A SUPERADMIN approves (the company is created, the applicant becomes ADMIN) or rejects (re-apply 24 h later, at most 3 per 30 days). Compare-and-set, audited.
8. **Joining a company.** The company ADMIN enables any combination:
   - **Invitation** (always available to the ADMIN): hashed single-use token, email, optional initial projects; accepting proves the address (sets `email_verified_at`).
   - **Join link** `/join/<key>`: enabled per company; the person submits a request, the ADMIN approves or rejects.
   - **Email domain**: the ADMIN claims a domain; a SUPERADMIN confirms it (free-mail providers are blocked, one domain belongs to one company). Mode `request` (ADMIN approves) or `auto` (only for a verified email; otherwise it falls back to `request`).
   Approval creates a USER membership and no project access; projects are assigned separately (ADR-0027 point 6 still holds for USER). Responses never reveal whether an email, company or link exists or is disabled.
9. **Company settings** (ADMIN, with instance values as defaults and floors): join modes, domains, support access; password minimum (never below the instance floor) and session days; completed-items visibility; project and item templates (company scope); the company's audit log; who may create projects (ADMINs or members); require two-factor for members; API token, OAuth and MCP access (allowed, maximum token lifetime); company profile (name, logo, default language, default workflow). A person in several companies gets the strictest value (longest password, shortest session, 2FA if any company requires it).
10. **Authorization.** `authz.go` stays the only decision point. Levels on a project: ADMIN of the project's company (or a valid full support grant) = every action; read-only support grant = `view`, `export`; USER with membership = member actions; everything else none (404 for invisible, 403 for a missing right). The SUPERADMIN account list shows accounts for suspension (email, name, status, company count), never a company's members or projects.
11. **Migration.** The existing default company stays. ADR-0028 point 4 already gave the SUPERADMIN an explicit ADMIN membership of it, so the current SUPERADMIN keeps access through that membership, not through the role (to be checked on the real data before the SUPERADMIN branch is removed). Existing memberships and projects are unchanged. A flag marks installations where an ADMIN of a company is missing (see ADR-0029 `allow_orphan`).

## Kept from ADR-0030

`users.status` and its single gate on every authentication path; suspension revokes sessions, tokens and OAuth grants and closes streams; compare-and-set transitions (reused for company applications and join requests); the non-disclosure rules; the 24 h / 3 per 30 days re-apply limits.

## Consequences

- Rework: `approval_by`, `request` registration mode, account pending/rejected UI and the admin Users approve/reject buttons; SUPERADMIN project authorization and its tests (matrix rows flip from allow to deny); `visibleUsersQuery`.
- New tables: `organization_settings`, `organization_domains`, `organization_join_requests`, `organization_applications`, `organization_invites`, `support_grants`, `email_verifications`; `users.email_verified_at`.
- Per-company policy touches login (password and 2FA rules), token creation and the MCP/OAuth gates.
- Domain auto-join and verification depend on outgoing mail (ADR-0012); without it every join is approved by the ADMIN.
