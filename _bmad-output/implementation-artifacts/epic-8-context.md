# Epic 8 Context: Privacidade e Conformidade (LGPD)

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Give Usuários control and transparency over their own personal data, and give Adms a safe way to process account-deletion requests, in compliance with LGPD. Usuários can export everything the system holds about them (identity, access log, movements they registered, orders they created). Account deletion is never self-service or immediate — it is a request an Adm reviews and confirms, and it anonymizes personal data (name, e-mail) while keeping the `usuario_id` intact across all historical Movimentações, Pedidos, and access-log entries, so auditability and referential integrity already established in prior epics are never broken.

## Stories

- Story 8.1: Exportação dos próprios dados pessoais
- Story 8.2: Exclusão e anonimização de dados pessoais por Adm

## Requirements & Constraints

- Any authenticated Usuário can trigger a personal-data export from their own profile ("Baixar meus dados"); the file (JSON or PDF) contains name, e-mail, their access-log entries, the Movimentações they registered, and the Pedidos they created. Sections with no records still render, empty — never an error.
- Account deletion is request-based, not self-service: a Usuário submits a request ("Solicitar exclusão de conta") from their own profile; only an `adm` can process it, outside the requesting Usuário's own interface — this is deliberate, to preserve the audit trail requirement below.
- Processing a deletion request anonymizes name and e-mail on the account. It must NOT delete or null the `usuario_id`, and must NOT alter, remove, or break any existing Movimentação, Pedido, or access-log row referencing that user — historical references stay fully intact and queryable.
- After anonymization, authentication with the old e-mail (password or SSO) must fail exactly as if the account didn't exist.
- Guardrail: an `adm` can never anonymize their own account or another `adm`'s account if doing so would leave the system with zero active `adm` accounts — block with an explanatory message that at least one active `adm` must always exist.
- Formal SLA/response-time process for LGPD requests is explicitly out of scope for this epic (deferred to legal/compliance) — do not invent one.
- Retention: exported data and deletion-request handling follow the same general retention posture as Movimentações/Pedidos history (12 months before archival) — no LGPD-specific retention rule beyond that exists in source material.

## Technical Decisions

- Role hierarchy is a total order enforced via a shared middleware rule: `adm=4 > gestor=3 > almoxarife=2 > usuario=1`, "actor can act on target" = `rank(actor) > rank(target)`. The last-active-admin guardrail is a check on top of this, not a replacement for it.
- Authorization decisions (allow/deny) belong in middleware; per-request scope filtering (e.g., a Usuário only ever sees/exports their own data, never another's) belongs in services, consuming the role/identity already resolved by middleware — never re-deriving it.
- Role is always read fresh from `usuarios.papel` in Postgres on every request (no cache) — relevant here because anonymization/deactivation must take effect immediately on the next request, with no stale-role window.
- IDs are UUID v4; dates are `timestamptz` UTC in the database, ISO 8601 in the API. E-mail is always normalized to lowercase before writes, with a unique index on the normalized value.
- HTTP error envelope is fixed: `{"error": {"code": string, "message": string}}`, using the shared code vocabulary (`FORBIDDEN`, `VALIDATION_ERROR`, `NOT_FOUND`, `CONFLICT`, etc.) — do not invent a new error code family for LGPD endpoints.
- Structured logging via stdlib `log/slog` (project-specific choice, diverges from `FB_APU02`).
- The access-log table (`logs_acesso`) is append-only — no endpoint may edit or delete rows in it; export and anonymization only ever read from it.
- No FR-39-specific ADR exists beyond "lives in `services/`, format follows the same conventions as the access-log format (AD-14)" — there is no dedicated export-file schema decision recorded; treat the export file shape (JSON or PDF, sections listed above) as the only fixed contract.

## UX & Interaction Patterns

- Both flows live inside Meu Perfil / Configurações → Privacidade (LGPD) tab: "Baixar meus dados" (any Usuário, self only) and "Solicitar exclusão de conta" (submits a request; the Usuário does not process it themselves).
- Export triggers a direct file generation/download — no intermediate confirmation screen needed (mirrors the "Baixar recibo" pattern from Pedidos: click, file downloads).
- Deletion processing (Adm side) must go through a `ConfirmDialog` before anonymizing — this is a destructive, irreversible action. Use the `destructive` color token (not `primary`) for the confirming action, consistent with how rejection/deletion/merge actions are styled elsewhere in the product; never use `primary` red for this.
- This is a support flow without a fully narrated user journey in the UX artifacts — IA and component/state patterns are defined (per above), but no key-screen mockup exists yet; implement against the acceptance criteria and these patterns directly.

## Cross-Story Dependencies

- Story 8.1 depends on data already produced by earlier epics: access-log entries (Epic 1, Story 1.12), Movimentações (Epic 5, Story 5.3), and Pedidos (Epic 7, Story 7.3) — it reads and packages them, it does not define their schema.
- Story 8.2's anonymization must not break the FK/history guarantees already established by those same epics (Movimentações, Pedidos, access log) — `usuario_id` references must survive untouched.
- Story 8.2 interacts with Epic 1's role/account-lifecycle machinery (account deactivation/demotion, role hierarchy) — the "last active adm" guardrail is an extension of that existing authorization model, not a new one.
