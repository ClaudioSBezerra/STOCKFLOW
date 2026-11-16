---
title: 'Logo da Empresa na Pré-Nota'
type: 'feature'
created: '2026-11-16'
status: 'done'
review_loop_iteration: 0
baseline_commit: 'f5a9d5d9f5facbc2f93cca0561bdd9d46d192120'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-pre-nota-layout-danfe.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O usuário quer a logo da Ferreira Costa impressa na Pré-Nota, mas o sistema não tem nenhum lugar para carregar uma logo — o PDF só mostra o nome da empresa em texto.

**Approach:** Logo por Empresa (vale para todas as Filiais), carregada numa nova tela **Cadastros > Empresa** por `gestor`+, guardada no banco, e desenhada no canto do cabeçalho da Pré-Nota (caixa da esquerda, acima da razão social e do CNPJ). Sem logo cadastrada o PDF fica exatamente como hoje (decisões do usuário, 2026-11-16).

## Boundaries & Constraints

**Always:**
- Armazenamento no banco (nova tabela `empresa_logos`: `empresa_id` UUID PK referenciando `empresas`, `tipo_conteudo` TEXT (`image/png` ou `image/jpeg`), `conteudo` BYTEA, `atualizado_em` TIMESTAMPTZ) — NÃO em `FOTOS_DIR` (volume diferente por ambiente e o Catálogo varre esse diretório). Uma logo por Empresa; enviar de novo substitui.
- Upload validado no servidor: só PNG ou JPEG, decodificado de verdade com `image.DecodeConfig` (nunca confiar em extensão nem em Content-Type do cliente), até 512 KB, largura e altura entre 32 e 4000 px. Fora disso -> 400 com mensagem clara; nada gravado.
- Rotas, todas `RequireAuth` + `RequireRole(PapelGestor)` e isoladas por Empresa (AD-20): `GET /e/{slug}/api/empresa/logo` (200 com os bytes e o `Content-Type`; 404 se não houver logo; `Cache-Control: private, no-cache` + ETag para a tela rever a troca), `PUT /e/{slug}/api/empresa/logo` (multipart, campo `logo`), `DELETE /e/{slug}/api/empresa/logo` (204; idempotente).
- Tela **Cadastros > Empresa** (`/cadastros/empresa`, item de menu `empresa`, `papelMinimo: 'gestor'`, gate de página `gestor`): mostra a logo atual (ou o estado "nenhuma logo"), botões Enviar/Trocar e Remover, pré-visualização antes de confirmar o envio, instruções de formato (PNG ou JPEG, até 512 KB, fundo transparente ou branco), e as mensagens de erro do servidor. Se já existir endpoint com os dados da Empresa da sessão (razão social, CNPJ), mostre-os só-leitura acima da logo; se não existir, não crie um endpoint novo só para isso.
- PDF: `RenderizarPreNotaPDF` recebe os bytes da logo no conteúdo (`PreNotaConteudo`, lidos por `MontarPreNotaConteudo` da `empresa_logos` da MESMA Empresa) e a desenha dentro da caixa esquerda do cabeçalho, no canto superior esquerdo, com altura máxima de ~40 pt e largura máxima de ~110 pt, mantendo a proporção; razão social e CNPJ continuam no texto, deslocados para baixo/ao lado da logo sem sobrepor. Sem logo, o cabeçalho é idêntico ao atual. Logo corrompida ou que o gopdf não consiga desenhar NUNCA derruba a geração do PDF: registra `slog.Warn` e segue sem a logo.
- Determinismo mantido: mesma entrada (incluindo os mesmos bytes de logo) -> mesmos bytes de PDF. A logo é lida do cadastro atual no momento do download (mesma regra de razão social/CNPJ/Filial).
- Pré-Notas já emitidas passam a sair com a logo se ela for cadastrada depois (consequência da regra acima, aceita).

**Ask First:** nenhuma — local, permissão e posição decididos pelo usuário.

**Never:** guardar a logo em `FOTOS_DIR`; aceitar SVG/GIF/WebP; redimensionar ou reencodar o arquivo enviado (guarda os bytes originais); logo por Filial; mexer no recibo de Pedido; expor a logo sem autenticação.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Enviar PNG válido | PNG 600x200, 80 KB | Gravada; tela mostra a logo; próxima Pré-Nota sai com ela | N/A |
| Enviar JPEG válido | JPEG 800x300 | Idem | N/A |
| Substituir | Já existe logo, envia outra | A nova substitui a antiga | N/A |
| Tipo inválido | SVG, GIF, PDF ou texto renomeado para .png | Nada gravado | 400 "use PNG ou JPEG" |
| Grande demais | 700 KB | Nada gravado | 400 (limite 512 KB) |
| Dimensões fora do limite | 10x10 px ou 5000x5000 px | Nada gravado | 400 |
| Remover | DELETE com logo existente / sem logo | 204 nos dois casos; PDF volta ao cabeçalho sem logo | N/A |
| Papel insuficiente | `almoxarife` em GET/PUT/DELETE | Nada feito | 403 |
| Outra Empresa | Gestor da Empresa A | Só enxerga/troca a logo da A | N/A |
| Sem logo | Empresa nunca enviou | GET 404; PDF idêntico ao atual | N/A |
| Logo ruim no banco | Bytes que o gopdf recusa | PDF gerado sem a logo + `slog.Warn` | N/A |

</frozen-after-approval>

## Code Map

- `backend/migrations/000062_*` (nova) -- cria `empresa_logos`; `.down.sql` remove. Testar migrando banco VAZIO do zero (subindo a API real) e o ciclo down/up.
- `backend/services/` (novo `empresa_logo.go` + teste) -- `SalvarLogoEmpresa`, `BuscarLogoEmpresa`, `RemoverLogoEmpresa` + validação (image.DecodeConfig, limites). Molde de upload: `enviarFotoProduto` (Story 3.5) e seu handler multipart.
- `backend/handlers/` + `backend/main.go` (rotas junto das de Filiais, ~linha 543) -- 3 rotas com `RequireRole(services.PapelGestor)`; teste HTTP (200/204/400/403/404) e teste pelo `newMux` REAL do RequireRole.
- `backend/services/pre_notas.go:100` (`MontarPreNotaConteudo`) e `:730` (`RenderizarPreNotaPDF`, desenho do cabeçalho) -- campo novo no conteúdo + desenho da logo com `pdf.ImageByHolder`/`pdf.Image` (gopdf v0.38.0 já está no go.mod); a posição do texto da caixa esquerda muda só quando há logo. O desenho dos quadros já sai de funções puras (`layoutDaPreNota`): inclua a logo e o novo posicionamento ali para ser testável sem ler bytes de PDF.
- `frontend/src/components/shell/nav-items.ts:117-120` -- novo item `{ id: 'empresa', label: 'Empresa', to: '/cadastros/empresa', papelMinimo: 'gestor' }` no grupo Cadastros; `frontend/src/App.tsx` (rota) e nova página em `frontend/src/pages/cadastros/` no molde de `FiliaisPage.tsx` (gate por `rankPapel`) com o componente da logo em `frontend/src/components/empresa/`.
- Testes de frontend: página (gate, estados, envio, troca, remoção, erro do servidor, pré-visualização) e `nav-items.test.ts`.

## Tasks & Acceptance

**Execution:**
- [x] `backend/migrations/000062_*.up/down.sql` -- tabela `empresa_logos`
- [x] `backend/services/empresa_logo.go` (+ `_test.go`) -- salvar/buscar/remover com validação; testes das 8 primeiras linhas da I/O Matrix + isolamento entre Empresas
- [x] `backend/handlers/` + `backend/main.go` (+ testes) -- GET/PUT/DELETE `/api/empresa/logo`, gestor+
- [x] `backend/services/pre_notas.go` (+ `_test.go`) -- logo no conteúdo e no desenho do cabeçalho; testes: com logo, sem logo (idêntico a hoje), logo corrompida não derruba o PDF, determinismo
- [x] Frontend: página Cadastros > Empresa, item de menu, rota, testes
- [x] Conferência visual: gerar PDF de amostra COM uma logo de teste (crie um PNG simples, tipo faixa colorida com o texto "FERREIRA COSTA", e um JPEG) e SEM logo, e abrir com a ferramenta Read para ver o cabeçalho; e, com backend + Vite locais (banco de teste novo e vazio), entrar como gestor, abrir Cadastros > Empresa, enviar a logo, ver a pré-visualização, trocar, tentar um SVG (deve recusar com mensagem) e remover

**Acceptance Criteria:**
- Given um Gestor, when envia uma logo PNG ou JPEG válida em Cadastros > Empresa, then ela é guardada e a Pré-Nota seguinte sai com a logo no canto do cabeçalho
- Given nenhuma logo cadastrada, when a Pré-Nota é gerada, then o PDF é idêntico ao de hoje
- Given `go build ./...`, `go test -p 1 ./...`, `npm run build` e `npx vitest run`, when rodados após a story, then terminam sem erro

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./services/... -run 'Logo|PreNota'` -- expected: build limpo, testes passando
- `cd backend && go test -p 1 ./...` -- expected: suíte completa limpa (DATABASE_URL do Postgres de teste, porta 55432; rodar UMA suíte só por vez)
- `cd frontend && npm run build && npx vitest run` -- expected: build limpo, suíte limpa

## Spec Change Log

- 2026-11-16 — Endurecimento aprovado pelo usuário (via coordenador), sem alterar o bloco frozen: além dos limites por lado (32 a 4000 px), o envio recusa imagem com mais de 4.000.000 de pixels totais (ex.: 2000×2000) para limitar a memória ao gerar o PDF. O envio também passou a decodificar a imagem inteira (corrompida/truncada -> 400) e a conferir, com a mesma lógica da geração do PDF, que o gopdf consegue desenhá-la (senão 400 na hora). Migration 000062 ganhou `CHECK (octet_length(conteudo) BETWEEN 1 AND 524288)`.

## Suggested Review Order

**Envio e validação (o ponto central)**

- Valida a imagem inteira (não só o cabeçalho), testa se o PDF consegue desenhá-la e aplica o teto de 4 milhões de pixels — o gestor descobre o problema na hora
  [`empresa_logo.go:60`](../../backend/services/empresa_logo.go#L60)

- Guarda os bytes ORIGINAIS (nunca reencoda); uma logo por Empresa
  [`empresa_logo.go:116`](../../backend/services/empresa_logo.go#L116)

- Tabela nova, com CHECK de tipo e de tamanho no banco
  [`000062_create_empresa_logos.up.sql`](../../backend/migrations/000062_create_empresa_logos.up.sql)

**Acesso (gestor+, por Empresa)**

- Três rotas atrás de `RequireRole(PapelGestor)`
  [`main.go:555`](../../backend/main.go#L555)

- PUT multipart (mensagem de tamanho vs. de formato), GET com ETag/304 e nosniff
  [`empresa_logo.go:91`](../../backend/handlers/empresa_logo.go#L91)

**PDF**

- Prepara a logo uma vez: PNG indexado com transparência parcial vira NRGBA só para desenhar; panico do gopdf não derruba o PDF
  [`pre_notas.go:551`](../../backend/services/pre_notas.go#L551)

**Tela Cadastros > Empresa**

- Pré-visualização, validação no navegador, "Tentar de novo", entrada de arquivo limpa
  [`LogoEmpresaSection.tsx:34`](../../frontend/src/components/empresa/LogoEmpresaSection.tsx#L34)

- Rota ligada ao menu (com teste do router real)
  [`App.tsx:166`](../../frontend/src/App.tsx#L166)
