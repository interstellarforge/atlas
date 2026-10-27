# ADR-0013: Issues, repository link and README

- Status: accepted
- Date: 2026-10-27

## Context

GIT 1 asks for an issues page per project with MCP tools, GIT 2 for a git repository linked to a project, GIT 3 for the repository's README shown in Atlas with working images and links.

## Decisions

### Issues

1. **An issue is a work item of type `bug`.** Not a second concept: items already have status, workflow, priority, assignee, labels, comments, checklists, links, notifications, audit, import/export and MCP. A second table would duplicate every rule.
2. **A project view `/p/KEY/issues`** is the list view with the type fixed to `bug`, a status filter next to priority, label and assignee, a "New issue" button that opens the normal item panel with type `bug`, and the same detail panel. No new endpoints: the list uses `GET /items?type=bug`.
3. **MCP: `issues_search`, `issue_create`, `issue_update`.** Thin adapters over `ListItems`, `CreateItem` and `UpdateItem` that fix the type. `issue_update` refuses a key that is not a bug (`core.RequireType`), takes the item version (optimistic locking as everywhere) and changes status through the normal workflow rules (transitions, required steps, WIP). Comments use the existing `comment_add`. They count against the same write quota as other tools. The generic tools keep working on bugs.

### Repository link

4. **Only a link is stored**: `projects.repo_url` (normalised `https://host/owner/name`, no `.git`, no credentials) and `repo_branch` (default `main`). The provider (GitHub, GitLab, Gitea/Codeberg/Forgejo, Bitbucket, other) is derived from the host. Atlas never clones, fetches git objects or stores secrets.
5. **Accepted forms:** `https://host/path`, `ssh://git@host/path` and `git@host:path`. Anything with a user or password in an https URL, other schemes, query strings, `..` and one-segment paths are refused (400).
6. **Right: `project.update`** (project admins and global admins), set through `PATCH /projects/{key}` with `repository: {url, branch}`, or `null` to unlink. The mutation is audited with actor and journalled in the same transaction. The link is part of the project in REST and in MCP `projects_list`. It is not part of item export/import (those carry items) nor of project templates (a template describes a workflow, not where the code lives).

### README

7. **The server fetches the README**, never the browser: `README.md` (then `readme.md`) from the provider's raw URL on the default branch, 10 s timeout, 1 MiB cap, https only, no credentials, no cookies. Cached in memory per project for 10 minutes; "refresh" is honoured at most every 15 seconds per project; the cache is keyed on repository and branch. A missing README, a failure and "not linked" are states of the answer, not HTTP errors.
8. **SSRF protection is at connection time.** The dialer refuses any address that is not a public internet address (loopback, private, link-local including cloud metadata, CGNAT, multicast, unspecified, IPv4-mapped IPv6), on the address actually dialled, so DNS rebinding and redirects cannot get around it. Redirects must stay on the same host over https. Proxies are ignored. `ATLAS_REPO_ALLOW_PRIVATE=true` switches the address check off for a self-hosted git server on a private network.
9. **Rewriting happens on the server, before rendering.** Relative image paths become the raw URL, relative links the provider's web page (`blob`/`src`) for the branch; paths never leave the repository root. The HTML READMEs lean on for layout (`<img>`, `<a>`, `<h1>`, `<br>`, `<p align>`, `<picture>`, bold/italic) is converted to its markdown equivalent or stripped, comments are dropped, and fenced code is left untouched. Links and images with other schemes (`javascript:`, `data:`, `file:`) are dropped; `http:` becomes `https:`.
10. **Images go through an authenticated proxy** (`GET /projects/{key}/readme/asset?u=`), not straight from the provider. The CSP stays `img-src 'self' data:`, readers do not leak their address to third parties, and SVG is contained: only hosts of the repository, its raw host, `*.githubusercontent.com` and a few badge/provider hosts are fetched (anything else stays a link), the data must really be an image (raster types are checked against their content, SVG is recognised by content), at most 5 MiB, and the response carries `Content-Security-Policy: default-src 'none'; sandbox` and `nosniff`. SVG only ever runs inside an `<img>`, where browsers do not execute scripts.
11. **Rendering uses the existing markdown pipeline** with raw HTML off, so the output only contains tags markdown-it generates itself. Tables, code blocks and badges work; Mermaid is not rendered (it would need a large dependency).

## Consequences

- A README that depends on raw HTML beyond the list above (iframes, tables written in HTML, `<details>` bodies' own markup) loses its layout but stays readable.
- Reference-style images (`![x][ref]`) are not rewritten; their definitions are treated as link targets.
- Private repositories are not supported: there are no credentials. A private repo shows "no README found".
- The README cache is process-local and lost on restart.
