# Multi-Tenancy Patterns in B2B SaaS — Research Digest

## Q1: Can one user account belong to more than one org/tenant? Common cardinality?

**Synthesis:** The dominant pattern in modern B2B/dev-tool SaaS is **1 user : many orgs/tenants**, with membership-scoped roles (a user can be an admin in one org and a regular member/analyst in another). This is true across GitHub, Linear, Vercel, and explicitly designed into multi-tenant auth platforms like Frontegg. Slack is a partial exception — its model treats "workspace membership" as loosely coupled to one account rather than a first-class multi-org membership list, but a single email can still join unlimited workspaces.

- claim: A GitHub user account can be a member of any number of organizations regardless of plan, and multiple personal accounts collaborate by joining the same org account.
  source: https://docs.github.com/en/organizations/collaborating-with-groups-in-organizations/about-organizations
  publisher: GitHub Docs
  pub_date: undated (evergreen doc)
  accessed: 2026-09-10
  confidence: high
  class: product doc

- claim: In Linear, a single user account can create/belong to multiple workspaces, each with independent membership and billing, switchable via the top-left workspace menu ("Switch workspace") or the `O then W` shortcut.
  source: https://linear.app/docs/workspaces
  publisher: Linear (official docs)
  pub_date: undated
  accessed: 2026-09-10
  confidence: high
  class: product doc

- claim: Linear explicitly recommends against one account spanning unrelated orgs for isolation reasons — suggesting a separate account (different email) for personal vs. work use, even though the platform technically allows multiple workspaces per account.
  source: https://linear.app/docs/workspaces
  publisher: Linear (official docs)
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: product doc

- claim: Vercel's `vercel switch` / `vercel teams switch` CLI command lets one logged-in account list and switch between all teams (tenants) it belongs to; the dashboard exposes the same via a team switcher in the top-left nav.
  source: https://vercel.com/docs/cli/switch
  publisher: Vercel (official docs)
  pub_date: undated
  accessed: 2026-09-10
  confidence: high
  class: product doc

- claim: In multi-tenant SaaS auth design (Frontegg), the standard model is "User → membership → account/tenant," where one user has multiple memberships (one per org), each carrying its own role.
  source: https://frontegg.com/blog/saas-multitenancy
  publisher: Frontegg
  pub_date: 2025-02-25 (updated 2026-08-26)
  accessed: 2026-09-10
  confidence: medium
  class: vendor blog

- claim: Slack lets one email join and toggle between an unlimited number of workspaces (free plan included) via the top-left workspace name or Ctrl/Cmd+number shortcuts.
  source: https://www.m.io/blog/how-to-handle-multiple-slack-workspaces-accounts
  publisher: Mio (independent, third-party blog)
  pub_date: undated
  accessed: 2026-09-10
  confidence: low
  class: independent analysis (not official Slack; directional only)

## Q2: Self-service signup vs. manual/platform-provisioned onboarding

**Synthesis:** Pure self-service (instant signup) is standard for horizontal PLG tools, while enterprise-oriented and vertical B2B SaaS more often uses admin-invited or admin-provisioned onboarding — historically manual, increasingly automated via self-serve admin portals and JIT (just-in-time) provisioning through SSO on first login. No source gave a hard statistic on prevalence by vertical.

- claim: In an admin-provisioning model, a customer-side administrator invites users rather than users self-signing-up, which gives the customer control over access — common in enterprise settings.
  source: https://workos.com/blog/b2b-saas-onboarding-organizations-users
  publisher: WorkOS
  pub_date: 2026-01-08
  accessed: 2026-09-10
  confidence: medium
  class: vendor blog

- claim: Manually configuring new customer tenants by hand is feasible only for a small customer base; as it grows, this becomes a bottleneck, pushing vendors toward self-serve admin portals for tenant setup.
  source: https://workos.com/blog/b2b-saas-onboarding-organizations-users
  publisher: WorkOS
  pub_date: 2026-01-08
  accessed: 2026-09-10
  confidence: medium
  class: vendor blog

- claim: JIT (just-in-time) provisioning — auto-creating an account on first SSO login — is often preferred over manual account creation for enterprise customers.
  source: https://workos.com/blog/b2b-saas-onboarding-organizations-users
  publisher: WorkOS
  pub_date: 2026-01-08
  accessed: 2026-09-10
  confidence: medium
  class: vendor blog

## Q3: "Platform owner / super admin" above each tenant's own admin?

**Synthesis:** Yes — well-established pattern: a small, restricted platform/super-admin role (internal to the vendor) with cross-tenant access (impersonation, billing, feature flags, aggregate analytics), structurally separate from each tenant's own customer/admin role (scoped to that tenant only).

- claim: Best-practice multi-tenant SaaS guidance recommends a distinct internal "Super Admin" role — able to access all tenants, impersonate users, manage cross-tenant billing, toggle feature flags, view aggregate analytics — restricted to a small number of internal staff (cited as 3-5 people).
  source: https://frontegg.com/blog/saas-multitenancy
  publisher: Frontegg
  pub_date: 2025-02-25 (updated 2026-08-26)
  accessed: 2026-09-10
  confidence: medium
  class: vendor blog

- claim: Architectural best practice separates "customer-admin permissions" from "platform-admin privileges" as a least-privilege design principle.
  source: https://frontegg.com/blog/saas-multitenancy
  publisher: Frontegg
  pub_date: 2025-02-25 (updated 2026-08-26)
  accessed: 2026-09-10
  confidence: medium
  class: vendor blog

- claim: In Atlassian Cloud, an "org admin"/"site admin" role can span multiple organizations/sites, distinct from ordinary in-product user roles, with an org-switcher showing only orgs the person administers.
  source: https://support.atlassian.com/atlassian-cloud/kb/site-administrator-role-in-the-centralized-user-management-and-original-user-management-experiences/
  publisher: Atlassian Support (official)
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: product doc

- claim: Microsoft Power Platform documents a distinct "service admin role" for managing an entire tenant environment, layered above per-app/per-environment admin roles.
  source: https://learn.microsoft.com/en-us/power-platform/admin/use-service-admin-role-manage-tenant
  publisher: Microsoft Learn (official)
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: product doc

## Q4: "Switch organization/workspace" UI pattern

**Synthesis:** Near-universal pattern: a workspace/org/team switcher anchored top-left, behind the current org name, opening a dropdown listing every org the account belongs to plus "create/join another." CLI-first tools (Vercel) mirror this with an explicit `switch` command.

- claim: Notion's workspace switcher lives at the top-left of the sidebar; clicking the workspace name opens a menu to switch workspaces, create a new one, add another account, or log out.
  source: https://www.notion.com/help/create-delete-and-switch-workspaces
  publisher: Notion (official Help Center)
  pub_date: undated
  accessed: 2026-09-10
  confidence: high
  class: product doc

- claim: Linear's workspace switcher is a dropdown reached by clicking the workspace name top-left, with a dedicated shortcut (`O` then `W`).
  source: https://linear.app/docs/workspaces
  publisher: Linear (official docs)
  pub_date: undated
  accessed: 2026-09-10
  confidence: high
  class: product doc

- claim: Vercel's team switcher sits top-left of the dashboard nav, mirrored in the CLI via `vercel switch`.
  source: https://vercel.com/docs/cli/switch
  publisher: Vercel (official docs)
  pub_date: undated
  accessed: 2026-09-10
  confidence: high
  class: product doc

- claim: Atlassian Cloud uses an org drop-down to move between organizations a user administers; clicking a site opens the Admin console scoped to that site.
  source: https://community.atlassian.com/forums/Jira-questions/As-admin-swap-between-sites/qaq-p/2299131
  publisher: Atlassian Community (forum)
  pub_date: undated
  accessed: 2026-09-10
  confidence: low
  class: independent (corroborated directionally by official Atlassian doc above)

## Leads not chased (budget-limited)
- Subdomain-per-tenant pattern (Zendesk-style) — not directly verified this session.
- Vertical WMS/ERP examples (NetSuite OneWorld, Odoo multi-company, SAP Business One) — most relevant analog to construction-materials inventory SaaS; not reached.
- Prevalence data (self-service vs. manual) specific to vertical B2B SaaS.

## Could not verify
- No official Slack documentation fetched (only third-party blogs).
- No hard stats on self-service vs. manual tenant provisioning prevalence by vertical.
- No primary NetSuite/Odoo/SAP Business One documentation retrieved.
