# Epic 6 Context: Normalização de Dados

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Give the Almoxarife tools to keep the product catalog clean without reviewing items one by one: automatic detection of dimensional inconsistencies (from legacy migration or names with implicit values not captured in structured fields), selective/batch application of corrections, detection of duplicate Products, and merging duplicates into a single record while fully preserving movement/order history and audit trail. This closes catalog-quality gaps inherited from the prototype (inconsistent free-text data, duplicate products on re-import).

## Stories

- Story 6.1: Detecção de inconsistências dimensionais
- Story 6.2: Aplicação seletiva de correções
- Story 6.3: Detecção de duplicatas
- Story 6.4: Mesclagem de duplicatas com trilha de auditoria

## Requirements & Constraints

- Access to Normalização (inconsistency + duplicate tools) requires role `almoxarife` or higher (role inheritance); a lower-privileged caller must be rejected on the server, not just hidden in the UI.
- Dimension fields are always structured (`{valor, unidade}`), never free text; a field already valid and structured must never generate a suggestion. Suggestions must always identify: product, field, suggested value, and origin (migration-derived or name-derived).
- "Ignore" on a suggestion is permanent only for that exact (product, field, value) combination — if the field later changes to a *different* inconsistent value, the suggestion must reappear. Corrections can be applied individually, batched per-product, or batched globally.
- Duplicate detection groups by: normalized name (accent-insensitive, case-insensitive, trimmed) + equivalent dimensions (with unit conversion) + coinciding locations. Products with the same normalized name but different dimensions must never be grouped as duplicates.
- The import report screen (Stories 3.3/3.4) must offer a "Verificar duplicatas agora" CTA that navigates directly into the duplicate-detection screen with analysis already running.
- Merging a duplicate group requires explicit confirmation naming which Product is kept (destructive/irreversible action — see UX pattern below). On confirm: quantities of the removed Products are summed into the kept Product; removed Products are soft-deleted (`deleted_at`); an audit record (who, when, which products removed, values) is written to a permanent table that is **never purged**, unlike the 12-month retention/purge policy applied to Movimentações/Pedidos history elsewhere in the system.
- Before soft-delete, `produto_id` on all historical Movimentações and Pedido-item rows belonging to removed Products must be rewritten to the surviving Product's id, preserving the system invariant "sum of Movimentações == current quantity" and keeping reports correct without needing to traverse merge lineage.
- Any Cart item or pending Pedido item referencing a Product that just got merged away must be automatically redirected to the surviving Product.
- The summed quantity shown during merge review must be revalidated at confirmation time — never applied from a stale snapshot taken when the group was first opened.
- A Product already soft-deleted by a prior merge must never re-enter a new merge/duplicate group; its photo remains on disk permanently for audit purposes even after soft-delete.
- Success criterion tied to this area: re-importing a spreadsheet must not produce new duplicates (validated by automated test).

## Technical Decisions

- Layered Go, no framework/ORM: normalization logic lives in `services/`, HTTP boundary in `handlers/normalizacao.go`; role-check decision happens in `middleware/` (never re-derived in a handler); any listing-scope filtering happens in `services/` consuming the role already resolved by middleware.
- Role hierarchy is total order (`adm=4 > gestor=3 > almoxarife=2 > usuario=1`); role is read from Postgres on every request, never cached.
- Every write to `produto_estoque.quantidade` (including the quantity consolidation done by a merge) must insert a corresponding `MOVIMENTACOES` row in the same transaction and use `SELECT ... FOR UPDATE`; when a transaction touches multiple `(produto_id, estoque_id)` pairs, lock acquisition order is the full set sorted ascending, not insertion order.
- Photos live in a persistent named Docker volume with versioned filenames (`<produto_id>-<timestamp_unix>.jpg`), never inline/base64; a soft-deleted Product's photo is kept on disk permanently for merge-audit reconstruction even though the Product row itself is excluded from all normal reads (`deleted_at IS NULL`).
- The merge audit table is exempt from the general 12-month retention/purge policy that applies to Movimentações/Pedidos.
- Real-time updates (SSE, in-process broadcaster) use a fixed event envelope `{"resource": "produtos"|"estoques"|"movimentacoes"|"pedidos", "id", "change": "created"|"updated"|"deleted"}`; a correction or merge affecting Produtos (and the Movimentações rewritten during merge) should emit on the corresponding channel(s) so open catalog views refresh — payload stays minimal, clients re-fetch via GET.

## UX & Interaction Patterns

- Inconsistency suggestions render as inline rows (product, field, suggested value) with inline "Aceitar"/"Ignorar" actions — never a modal for a simple single correction. Batch acceptance uses checkbox selection + "Aplicar selecionadas"; applied/accepted suggestions disappear from the list immediately without a manual page reload.
- Merge is a destructive/irreversible action and must use the `AlertDialog` component (shadcn) for confirmation — this codebase deliberately avoids the native `window.confirm()` pattern used by the legacy prototype for all destructive actions (reject Pedido, merge/delete Product, deactivate account).
- Normalização sits in the desktop rail navigation with a vertical submenu (224px, Inconsistências / Duplicatas); on mobile it's tucked under "Mais" (bottom sheet), consistent with Estoques.
- The import-report screen's "Verificar duplicatas agora" CTA must land the user directly in Normalização → Duplicatas with the analysis already in progress, not just link to the empty tool.

## Cross-Story Dependencies

- Story 6.1 (detection) feeds Story 6.2 (apply/ignore); Story 6.2's "ignore" persistence is keyed on the exact (product, field, value) tuple produced by 6.1's analysis.
- Story 6.3 (duplicate detection) feeds Story 6.4 (merge); 6.4 depends on 6.3's grouping output and must exclude any Product already soft-deleted by a prior 6.4 run.
- Story 3.7 (migration dimension conversion) is a source of "could not be auto-converted" values that Story 6.1 must surface.
- Stories 3.3/3.4 (import report) link forward into Story 6.3 via the "Verificar duplicatas agora" CTA.
- Story 6.4 touches Epic 7 (Pedidos/Carrinho): a merge must redirect any Cart item or pending Pedido item pointing at a removed Product to the surviving one (this is also referenced from Story 7.1's own acceptance criteria as the trigger for auto-removing stale cart items when a redirect target no longer resolves).
- Story 6.4 depends on the Movimentações/Pedido-item schema and the "sum of Movimentações == quantidade atual" invariant relied on by Epic 5 (Movimentações) and Story 4.x reporting features.
