---
title: 'CNPJ alfanumérico (Empresa e Filial)'
type: 'feature'
created: '2026-11-16'
status: 'done'
review_loop_iteration: 0
baseline_commit: '7a67b25b4429fa6745661f735fceb8ac7341086c'
context: ['{project-root}/_bmad-output/implementation-artifacts/spec-filial-dados-fiscais.md']
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Desde julho de 2026 a Receita emite CNPJ alfanumérico (letras A-Z e números nas 12 primeiras posições, ex. `12.ABC.345/01DE-35`). O stockflow só aceita CNPJ com 14 dígitos numéricos: `NormalizarCNPJ` joga fora as letras, então uma Empresa ou Filial nova com o formato novo é recusada (ou, pior, teria as letras removidas em silêncio e o dígito verificador deixaria de bater).

**Approach:** Passar a aceitar os dois formatos (numérico antigo e alfanumérico novo) em todo lugar que valida, grava, compara ou exibe CNPJ — Empresa, Filial, raiz da Filial, PDF da Pré-Nota e telas — sem migration (a coluna `cnpj CHAR(14)` já comporta letras) e sem afetar nenhum CNPJ numérico já cadastrado.

## Boundaries & Constraints

**Always:**
- Regra do dígito verificador (Receita): cada caractere das 12 primeiras posições vale seu código ASCII menos 48 (`0`-`9` = 0-9, `A` = 17, `B` = 18, ..., `Z` = 42); mesmos pesos e mesmo módulo 11 do cálculo atual; os 2 últimos caracteres (dígitos verificadores) são SEMPRE numéricos. Todo CNPJ numérico válido hoje continua válido, porque para dígitos o valor é o mesmo.
- Normalização: tira máscara e espaços (`. / -` e brancos), converte letras para MAIÚSCULAS e só aceita `[0-9A-Z]`; qualquer outro caractere -> recusa com mensagem clara (nunca descartar em silêncio). Resultado tem sempre 14 caracteres.
- Estrutura: posições 1-12 `[0-9A-Z]`, posições 13-14 `[0-9]`; continua recusando CNPJ com os 14 caracteres iguais.
- Raiz (8 primeiros caracteres) e comparação de raiz Filial x Empresa passam a trabalhar com os 8 primeiros caracteres alfanuméricos, não só dígitos.
- Unicidade: Empresa e Filial comparam o valor normalizado (maiúsculo), então `ab...` e `AB...` são o mesmo CNPJ.
- Máscara de exibição igual à atual (`00.000.000/0000-00`), agora aplicada a 14 caracteres alfanuméricos — no backend (PDF da Pré-Nota) e no frontend (telas).
- CEP continua só dígitos, com helper próprio (não tocar).

**Ask First:** nenhuma — formato e algoritmo são os publicados pela Receita Federal (CNPJ alfanumérico, em vigor desde 07/2026) e a decisão de fazer esta story foi do usuário em 2026-11-16.

**Never:** migration (coluna já é `CHAR(14)`); mudar a regra de unicidade da Empresa nem a da raiz; reescrever CNPJs já gravados; mexer em CPF ou em qualquer outro documento.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| CNPJ numérico antigo | `38.149.716/0001-28` | Aceito, igual a hoje | N/A |
| CNPJ alfanumérico válido | `12.ABC.345/01DE-35` (dígitos calculados pela regra da Receita) | Aceito, gravado `12ABC34501DE35` | N/A |
| Minúsculas | `12.abc.345/01de-35` | Aceito, gravado em maiúsculas | N/A |
| Dígito verificador errado | alfanumérico com DV trocado | Recusado | 400 "dígito verificador não confere" |
| Letra na posição do DV | `...01DE-3A` | Recusado | 400 |
| Caractere inválido | `12.ABC.345/01D@-35` | Recusado, não limpa em silêncio | 400 |
| Tamanho errado | 13 ou 15 caracteres | Recusado | 400 "CNPJ deve ter 14 caracteres" |
| Filial de empresa alfanumérica | Empresa `12ABC345...`; Filial com a mesma raiz `12ABC345` | Aceita | N/A |
| Filial com raiz diferente | raiz `12ABC346` numa Empresa `12ABC345` | Recusada | 400 (mensagem da raiz) |
| Duplicado em caixa diferente | Empresa/Filial já tem `12ABC...`, tenta `12abc...` | Recusado | 409 |
| PDF | Filial com CNPJ alfanumérico | Quadro mostra `12.ABC.345/01DE-35` | N/A |

</frozen-after-approval>

## Code Map

- `backend/services/empresas.go:121-170` (`NormalizarCNPJ`, `pesosCNPJ`, `digitoVerificadorCNPJ`, `ValidarCNPJ`) -- núcleo: `NormalizarCNPJ` passa a manter `[0-9A-Z]` (maiúsculo) e a recusar o resto; `digitoVerificadorCNPJ` usa `código ASCII - 48`; `ValidarCNPJ` exige posições 13-14 numéricas. `NormalizarCNPJ` hoje é reusado em `empresas.go:322` para o CEP — o CEP passa a usar o helper próprio de dígitos que a Filial já tem (`filiais.go`), para a mudança não vazar.
- `backend/services/empresas.go:300` (`validarDadosEmpresa`, `v.cnpj = NormalizarCNPJ(d.CNPJ)`) -- usa o novo normalizador/validador.
- `backend/services/filiais.go:188-236` (`raizCNPJ`, `validarDadosFilial`, `formatarRaizCNPJ`) -- raiz = 8 primeiros caracteres alfanuméricos; comparação e mensagem de raiz.
- `backend/services/pre_notas.go:278-285` (`formatarCNPJPreNota`) -- máscara sobre 14 caracteres alfanuméricos (hoje já indexa por posição, só trocar a premissa/comentário e testar).
- `frontend/src/lib/formatacao.ts:7-13` (`formatarCNPJ`, `replace(/\D/g,'')`) -- aceitar letras (maiúsculas) ao formatar.
- `frontend/src/components/filiais/FiliaisSection.tsx` e `frontend/src/pages/plataforma/EmpresasPage.tsx:471` -- campos de CNPJ não podem filtrar só dígitos; converter para maiúsculas ao digitar; placeholder/ajuda mostrando que aceita letras.
- `frontend/src/lib/plataforma.ts` -- reexporta `formatarCNPJ`; conferir se há validação/máscara de CNPJ própria.
- `backend/cmd/migrar-multi-empresa/main.go:98,201` -- recebe CNPJ por flag e passa ao service; só conferir que o texto da flag/ajuda não promete "só dígitos".
- Testes: ajustar os que assumem "só dígitos" (empresas, filiais, pre_notas, plataforma.test.ts) e cobrir a I/O Matrix. Calcular os dígitos verificadores dos exemplos alfanuméricos com a própria regra (não copiar de fonte externa), e incluir pelo menos um vetor conferido contra a definição publicada (valor = ASCII - 48).

## Tasks & Acceptance

**Execution:**
- [x] `backend/services/empresas.go` (+ `_test.go`) -- normalização, dígito verificador e validação alfanuméricos; CEP desacoplado do normalizador de CNPJ; testes das linhas de validação da I/O Matrix (aceita antigo, aceita novo, minúsculas, DV errado, letra no DV, caractere inválido, tamanho errado)
- [x] `backend/services/filiais.go` (+ `_test.go`) -- raiz alfanumérica, comparação e unicidade em maiúsculas; testes das linhas de Filial da I/O Matrix
- [x] `backend/services/pre_notas.go` (+ `_test.go`) -- máscara de CNPJ alfanumérico no quadro; teste
- [x] `frontend/src/lib/formatacao.ts`, `FiliaisSection.tsx`, `EmpresasPage.tsx`, `lib/plataforma.ts` (+ testes) -- formatar e digitar CNPJ alfanumérico
- [x] Conferir `backend/cmd/migrar-multi-empresa/main.go` (texto da flag) e demais pontos que o grep de `NormalizarCNPJ`/`formatarCnpj` encontrar
- [x] Conferência visual: gerar PDF de amostra com Remetente/Destinatário de CNPJ alfanumérico e abrir com Read; e, com backend + Vite locais (banco de teste vazio), cadastrar uma Filial com CNPJ alfanumérico pela tela e ver a tabela e a edição mostrando a máscara certa

**Acceptance Criteria:**
- Given uma Filial (ou Empresa) com CNPJ alfanumérico válido, when o adm cadastra, then é aceito, gravado em maiúsculas e exibido com a máscara `00.000.000/0000-00` nas telas e no PDF
- Given qualquer CNPJ numérico já cadastrado, when o sistema sobe com esta mudança, then continua válido e nada é regravado
- Given `go build ./...`, `go test -p 1 ./...`, `npm run build` e `npx vitest run`, when rodados após a story, then terminam sem erro

## Verification

**Commands:**
- `cd backend && go build ./... && go test ./services/... -run 'CNPJ|Filial|PreNota|Empresa'` -- expected: build limpo, testes passando
- `cd backend && go test -p 1 ./...` -- expected: suíte completa limpa (DATABASE_URL do Postgres de teste, porta 55432)
- `cd frontend && npm run build && npx vitest run` -- expected: build limpo, suíte limpa

## Spec Change Log

- **2026-11-16, revisão (patches, sem loopback):** revisão em triplo sem intent_gap/bad_spec. A validação é só alcançável depois da checagem de estrutura (`digitoVerificadorCNPJ` só é chamado por `ValidarCNPJ`) e não existe restrição de dígitos em `cnpj` no banco, então achados sobre migration/outros chamadores foram descartados por verificação. Aplicados: comentários desatualizados, `formatarCNPJ` não mascara valor com símbolo inválido, maiúscula só ASCII (`ß`), campos de CNPJ iguais nas duas telas, vetores de teste calculados fora do código (DV 0 e DV 9), entradas não-ASCII, e validação de estrutura contando runas. **KEEP:** algoritmo ASCII-48/pesos/módulo 11 (conferido por cálculo independente em Python), CEP desacoplado via `soDigitos`, nenhuma migration.

## Suggested Review Order

**Validação (o ponto central)**

- Normaliza (tira máscara, maiúscula) sem descartar caractere inválido; quem valida recusa
  [`empresas.go:126`](../../backend/services/empresas.go#L126)

- Dígito verificador: valor = ASCII - 48, mesmos pesos e módulo 11 — para dígitos dá o mesmo resultado de sempre
  [`empresas.go:162`](../../backend/services/empresas.go#L162)

- Estrutura (12 posições [0-9A-Z] + 2 dígitos) e comparação dos dígitos
  [`empresas.go:179`](../../backend/services/empresas.go#L179)

- CEP desacoplado do normalizador de CNPJ
  [`empresas.go:142`](../../backend/services/empresas.go#L142)

**Filial**

- Raiz = 8 primeiros caracteres alfanuméricos; compara com a raiz da Empresa
  [`filiais.go:190`](../../backend/services/filiais.go#L190)
  [`filiais.go:206`](../../backend/services/filiais.go#L206)

**PDF e telas**

- Máscara no quadro Remetente/Destinatário
  [`pre_notas.go:280`](../../backend/services/pre_notas.go#L280)

- Formatadores: maiúscula só ASCII, limpeza e máscara sem mascarar símbolo inválido
  [`formatacao.ts:10`](../../frontend/src/lib/formatacao.ts#L10)
  [`formatacao.ts:28`](../../frontend/src/lib/formatacao.ts#L28)

- Campos de CNPJ de Empresa e Filial
  [`EmpresasPage.tsx`](../../frontend/src/pages/plataforma/EmpresasPage.tsx)
  [`FiliaisSection.tsx`](../../frontend/src/components/filiais/FiliaisSection.tsx)
