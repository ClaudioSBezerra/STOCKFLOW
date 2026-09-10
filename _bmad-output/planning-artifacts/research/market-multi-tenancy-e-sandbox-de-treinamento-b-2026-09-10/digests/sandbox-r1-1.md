# Research Digest: Per-Customer Training/Sandbox Environments in B2B SaaS

## Q1: Architectural patterns — separate mirror tenant vs. same-tenant mode/flag

**Synthesis:** The dominant pattern among mature B2B SaaS/enterprise platforms (Salesforce, NetSuite, Workday, HubSpot) is a fully separate, dedicated org/tenant/instance provisioned as a copy of production metadata (and optionally data), with its own login and refresh lifecycle — not a flag inside the live tenant. The "same-tenant training mode" pattern appears mainly in simpler/prosumer tools (QuickBooks sample company, in-app dummy data) rather than complex ERP/CRM/WMS systems.

- claim: Salesforce sandboxes are provisioned as fully distinct orgs that copy metadata (and, for two of the four types, data) from production, refreshed on a schedule rather than being a mode flag inside the production org.
  source: https://www.salesforce.com/platform/sandboxes-environments/salesforce-sandbox-guide/
  publisher: Salesforce
  pub_date: undated
  accessed: 2026-09-10
  confidence: high
  class: product doc

- claim: Workday distinguishes three tenant types — Production, Sandbox, and Sandbox Preview — where Sandbox is a copy of production data refreshed weekly, and Sandbox Preview is a production copy plus upcoming-release functionality.
  source: https://doc.workday.com/workday-education/en-us/course-manuals/hcm-core-for-administrators/workday-tenants-and-tools.html
  publisher: Workday
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: product doc (via search snippet)

- claim: Best-practice multi-tenant SaaS architecture treats demo/sandbox tenant creation as an automated, infrastructure-as-code provisioning workflow orchestrated by a central control plane.
  source: https://aws.amazon.com/blogs/apn/tenant-onboarding-best-practices-in-saas-with-the-aws-well-architected-saas-lens
  publisher: AWS Partner Network Blog
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: vendor blog (snippet only)

- claim: QuickBooks bundles a sample company file (separate, not a linked flag) for training, plus a "Test Drive" mode that resets on every login and never persists data.
  source: https://quickbooks.intuit.com/learn-support/en-us/other-questions/create-a-test-company/00/632216
  publisher: Intuit / QuickBooks Community
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: product community doc

## Q2: Concrete examples across vendors

**Synthesis:** Salesforce's mechanism is the clearest reference model: four sandbox types differing by data copy and refresh cadence, each with its own login URL. NetSuite uses one "Sandbox Account" refreshed on-demand from production. Workday uses "Sandbox"/"Sandbox Preview". HubSpot gates a full "Standard Sandbox" behind Enterprise tier, plus free time-boxed dev/test accounts. ServiceNow calls these "sub-production instances" with strict naming rules.

- claim: Salesforce's four sandbox types are Developer (200MB, metadata only, daily refresh), Developer Pro (1GB, metadata only, daily refresh), Partial Copy (5GB, metadata + templated data sample, refresh every 5 days), Full Copy (production-equivalent storage, full data replica, refresh every 29 days).
  source: https://www.salesforce.com/platform/sandboxes-environments/salesforce-sandbox-guide/
  publisher: Salesforce
  pub_date: undated
  accessed: 2026-09-10
  confidence: high
  class: product doc

- claim: Salesforce sandboxes are created via Setup > New Sandbox, choosing a type and name; naming convention like "UAT_Portal2025" is recommended.
  source: https://www.salesforce.com/platform/sandboxes-environments/salesforce-sandbox-guide/
  publisher: Salesforce
  pub_date: undated
  accessed: 2026-09-10
  confidence: high
  class: product doc

- claim: NetSuite sandbox refresh is manual/admin-triggered (Setup > Company > Sandbox Accounts > Refresh Sandbox), fully overwriting sandbox data from production; 2026.1 adds refresh-while-accessible and sandbox-from-sandbox refresh for higher tiers.
  source: https://gurussolutions.com/resources/netsuite-faqs/netsuite-sandbox
  publisher: Gurus Solutions (NetSuite partner)
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: independent/partner doc

- claim: HubSpot offers a Standard Sandbox (Enterprise-tier, full CRM duplicate) distinct from free 90-day Developer/Test accounts and CLI-managed Development sandboxes.
  source: https://developers.hubspot.com/docs/getting-started/account-types
  publisher: HubSpot
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: product doc

- claim: ServiceNow's naming policy for "sub-production" instances forbids "demo"/"poc"/"pov" suffixes, requiring alphanumeric-only names.
  source: https://www.servicenow.com/community/it-service-management-articles/how-to-request-an-instance-rename-for-a-sub-production-instance/ta-p/2314166
  publisher: ServiceNow Community
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: vendor community doc

## Q3: Naming conventions and activation model

**Synthesis:** Naming clusters around "Sandbox" (Salesforce, NetSuite, HubSpot, Workday), with "UAT," "Test Drive," "Developer/Test account," "Sandbox Preview" as sub-variants. Activation is generally tier-gated, not on-by-default: Salesforce sandbox types/counts bound to production edition; HubSpot Standard Sandbox needs Enterprise; NetSuite's advanced refresh limited to Premium/Enterprise/Ultimate. Refresh cadence is the built-in staleness limit (daily for no-data sandboxes, up to 29 days for full-data copies).

- claim: Refresh intervals scale inversely with production data held — no-data Salesforce sandboxes refresh daily, data-bearing ones every 5 or 29 days.
  source: https://www.salesforce.com/platform/sandboxes-environments/salesforce-sandbox-guide/
  publisher: Salesforce
  pub_date: undated
  accessed: 2026-09-10
  confidence: high
  class: product doc

- claim: HubSpot gates full-featured Standard Sandbox behind Enterprise tier; lighter dev/test accounts are free but time-boxed (90 days).
  source: https://developers.hubspot.com/docs/getting-started/account-types
  publisher: HubSpot
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: product doc

- claim: NetSuite's newest sandbox-refresh capabilities are restricted to Premium/Enterprise/Ultimate tiers.
  source: https://gurussolutions.com/resources/netsuite-faqs/netsuite-sandbox
  publisher: Gurus Solutions
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: independent analysis

- claim: QuickBooks Online's "Test Drive" is opt-in, no subscription needed, never persists data (resets every login).
  source: https://gentlefrog.com/quickbooks-test-drive/
  publisher: Gentle Frog Bookkeeping and Custom Training
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: independent analysis

## Q4: UX patterns distinguishing sandbox from production

**Synthesis:** Strongest verified pattern: URL/subdomain marking — Salesforce injects "sandbox" into the My Domain hostname. An in-app sandbox banner exists (inferred from a feature request to make its color customizable, implying a fixed default today); third-party extensions exist specifically because the built-in indicator is seen as insufficient.

- claim: When Salesforce Enhanced Domains are enabled, "sandbox" is automatically inserted into My Domain URLs for sandbox orgs.
  source: https://help.salesforce.com/s/articleView?id=platform.domain_mgmt_sandbox_custom_domains.htm&language=en_US&type=5
  publisher: Salesforce Help
  pub_date: undated
  accessed: 2026-09-10
  confidence: medium
  class: product doc (title/snippet-level)

- claim: Salesforce admins commonly find the default built-in sandbox banner insufficient across multiple sandboxes and resort to third-party browser extensions for custom-colored headers.
  source: https://chromewebstore.google.com/detail/org-header-for-salesforce/bhnfbnhpmnfacebccnjjoeecgmnbhjam?hl=en
  publisher: Chrome Web Store listing
  pub_date: undated
  accessed: 2026-09-10
  confidence: low
  class: independent (listing name inference only)

- claim: An active Salesforce IdeaExchange feature request asks to allow changing the sandbox banner color, implying a fixed non-customizable default today.
  source: https://ideas.salesforce.com/s/idea/a0B8W00000GdpKiUAJ/change-color-of-sandbox-banner
  publisher: Salesforce IdeaExchange
  pub_date: undated
  accessed: 2026-09-10
  confidence: low
  class: vendor community (existence/title confirmed via search only)

## Leads not chased (budget-limited)
- SAP-specific mechanism (S/4HANA Test/QA via SAP LaMa, or BTP trial subaccounts) not retrieved.
- Direct fetch of Salesforce banner default color/copy failed to load.
- WMS-specific examples (Manhattan Associates, Blue Yonder, Fishbowl) not searched — most relevant analog to construction-materials stock control.
- HubSpot's exact sandbox creation trigger (auto vs manual at onboarding) not confirmed beyond tier-gating.

## Could not verify
- Vendor rationale for choosing mirror-tenant vs. same-tenant-flag (inferred from behavior, not stated design docs).
- Exact default Salesforce banner color/text.
- Per-customer sandbox count limits for NetSuite/Workday/HubSpot.
