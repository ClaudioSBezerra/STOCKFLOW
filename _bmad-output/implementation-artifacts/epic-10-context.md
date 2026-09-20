# Epic 10 Context: Enriquecimento do Cadastro de Produto e Configuração de Catálogo

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Tornar o cadastro de Produto mais completo e consistente (nome mínimo, Template de Nomenclatura sempre obrigatório com fallback "Genérico", código de Produto automático, Código do Fornecedor/EAN-13, Unidade de Medida e Embalagem, colunas explícitas na listagem do Catálogo) e dar ao `adm`+ autonomia para manter Categorias e Templates de Nomenclatura próprios por Empresa, sem depender de seed fixo. Nasce de feedback real do primeiro ciclo de treinamento pós Multi-Empresa.

## Stories

- Story 10.1: Nome mínimo e Template de Nomenclatura obrigatório com fallback Genérico
- Story 10.2: Código de Produto automático e sequencial por Empresa
- Story 10.3: Código do Fornecedor, EAN-13, Unidade de Medida e Embalagem
- Story 10.4: Colunas explícitas na listagem do Catálogo
- Story 10.5: CRUD de Categorias
- Story 10.6: CRUD de Templates de Nomenclatura

## Requirements & Constraints

- Nome de Produto: mínimo 10 e máximo 255 caracteres, validado no cadastro e em toda edição.
- Template de Nomenclatura é obrigatório em todo cadastro (28 estruturais + "Genérico"); a edição de nome revalida contra o template aplicado.
- Regras novas valem só para cadastro novo e próxima edição de cada Produto; nenhuma varredura ou reedição retroativa de Produtos legados.
- Código de Produto é gerado pelo sistema (não editável, zero-padding de 6 dígitos, sequência independente por Empresa). Códigos manuais antigos permanecem intactos, sem renumeração.
- Código do Fornecedor (texto livre) e EAN-13 são opcionais, sem unicidade e sem relação com o código interno nem com o Código de Identificação usado no scanner (FR-35). EAN-13 informado é validado (13 dígitos + dígito verificador); vazio nunca é rejeitado.
- Unidade de Medida é obrigatória só para Produto novo; Embalagem é opcional (texto livre, ex. "CX 24"). Ambas retornam como campos próprios no detalhe do Produto.
- Categorias: código até 8 caracteres, nome/descrição até 50, com limites aplicados no banco. Templates: cadastro com estrutura de tokens.
- Exclusão de Categoria ou Template referenciado por Produto é bloqueada (mesmo princípio de FR13/AD-31). Editar Template em uso nunca é retroativo.
- CRUD de Categoria e Template exige papel `adm`+ (403 abaixo disso). As ~25 Categorias seed por Empresa permanecem intactas e passam a ser editáveis, sem migração de dado.
- "Genérico" aparece na lista como qualquer template, mas a UI não permite excluí-lo enquanto for o fallback das categorias sem template específico.

## Technical Decisions

- Isolamento por Empresa (AD-20): `empresa_id` resolvido no middleware e nunca aceito do cliente; Categorias e Templates são cópia editável independente por Empresa.
- Código de Produto (AD-26): tabela `contadores_produto` (`empresa_id` PK, `proximo_valor`), incrementada por `UPDATE ... RETURNING` atômico na mesma transação do `INSERT` em `produtos`. A linha inicial nasce na transação de provisionamento da Empresa (Story 9.2), nunca por lazy-init.
- Colunas novas em `produtos` (AD-32): `codigo_fornecedor`, `ean13` (CHAR(13)), `unidade_medida` (enum do addendum §F), `embalagem`. Migração aditiva: `unidade_medida` nasce nullable, backfill em lote com valor único `[ASSUMPTION]` "un" para Produtos legados, e só depois a obrigatoriedade vale no cadastro/edição. Dado legado incompleto nunca impede o sistema de subir.
- Template "Genérico" (AD-34): linha seed com marcador `[NOME LIVRE]`, tratada como caso especial no motor de validação (aceita qualquer texto não vazio, sem checar ordem/token). Os demais templates seguem validação estrutural.
- CRUD de Categoria/Template (AD-33): handlers em `handlers/categorias.go` e `handlers/nomenclatura.go`, com serviços correspondentes. Bloqueio de exclusão por `produtos.categoria_id`/`produtos.template_id`. Confirmação de exclusão via `ConfirmDialog`.

## UX & Interaction Patterns

- Catálogo tem duas visões: grade (cards) e tabela agrupada (`CatalogoGrupo`, agrupa por mesmo nome+dimensões, expansível por Estoque). Ambas exibem explicitamente código, nome, categoria, estoque total (soma across Estoques) e embalagem+unidade como colunas próprias, sem precisar expandir.
- Embalagem ausente mostra traço/vazio sem quebrar o layout.
- Em linha agrupada cujos Produtos divergem em código, categoria ou embalagem+unidade, a coluna mostra "Múltiplos"; se todos concordam, mostra o valor comum. A chave de agrupamento existente (Story 4.3) não muda.
- Campo de código no formulário de cadastro é só exibição, preenchido após a criação.
- Badges de status usam ícone + texto, nunca só cor.

## Cross-Story Dependencies

- 10.4 consome `unidade_medida`/`embalagem` persistidos pela 10.3 e só pode rodar depois dela (o scheduler do `bmad-loop` não impõe essa ordem se a 10.3 travar).
- 10.2 depende do provisionamento de Empresa da Story 9.2 (linha inicial de `contadores_produto`).
- 10.5 e 10.6 dependem do seed de Categorias/Templates por Empresa do Epic 9 e do template "Genérico" definido na 10.1.
- 10.1 e 10.6 compartilham a regra de validação de nome contra template: o CRUD de Templates altera o que a 10.1 valida, sempre sem efeito retroativo.
- Consumidores posteriores: o Epic 11 (Lote/Validade) e o Epic 12 (Filiais/Centro de Custo) seguem o mesmo molde de migração aditiva.
