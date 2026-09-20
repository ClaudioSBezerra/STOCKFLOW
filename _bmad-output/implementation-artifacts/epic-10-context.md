# Epic 10 Context: Enriquecimento do Cadastro de Produto e Configuração de Catálogo

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Almoxarife passa a cadastrar Produtos mais completos e consistentes: nome minimamente descritivo, Template de Nomenclatura sempre obrigatório (com fallback Genérico para categorias sem padrão estrutural), Código do Fornecedor e EAN-13, Unidade de Medida e Embalagem, e colunas explícitas na listagem do Catálogo. Em paralelo, `adm`+ passa a manter Categorias e Templates de Nomenclatura como cópias próprias e editáveis por Empresa, em vez de depender de um seed fixo compartilhado. É a resposta direta ao primeiro ciclo real de treinamento de usuários finais pós Multi-Empresa (Epic 9).

## Stories

- Story 10.1: Nome mínimo e Template de Nomenclatura obrigatório com fallback Genérico
- Story 10.2: Código de Produto automático e sequencial por Empresa
- Story 10.3: Código do Fornecedor, EAN-13, Unidade de Medida e Embalagem
- Story 10.4: Colunas explícitas na listagem do Catálogo
- Story 10.5: CRUD de Categorias
- Story 10.6: CRUD de Templates de Nomenclatura

## Requirements & Constraints

- Nome de Produto exige mínimo de 10 caracteres (máximo 255 mantido), revalidado tanto no cadastro quanto em toda edição posterior — não dá para burlar editando depois.
- Template de Nomenclatura passa a ser obrigatório; o fallback "Genérico" (`[NOME LIVRE]`) é sempre uma opção e aceita qualquer texto não vazio, cobrindo as ~16 das 25 categorias sem template estrutural específico. Nunca pode ser excluído enquanto for o único fallback disponível.
- Nenhuma regra nova de obrigatoriedade (nome mínimo, template, Unidade de Medida) dispara varredura retroativa: vale só para cadastro novo e para a próxima edição de cada Produto já existente.
- Código do Produto deixa de ser digitado: gerado automaticamente, sequencial e zero-padded (6 dígitos), com sequência independente por Empresa; códigos manuais legados permanecem intactos, sem renumeração.
- Código do Fornecedor (texto livre) e EAN-13 são opcionais, sem checagem de unicidade, sem relação funcional com o código interno do Produto nem com o Código de Identificação já usado por QR Code/código de barras. EAN-13 é validado por formato (13 dígitos + dígito verificador) só quando informado.
- Unidade de Medida é obrigatória apenas para Produto novo; Embalagem é sempre opcional. Produto legado sem Unidade de Medida recebe valor padrão único via migração antes de a obrigatoriedade valer.
- Listagem do Catálogo (grade e tabela agrupada) expõe código, nome, categoria, estoque total (soma entre Estoques) e embalagem+unidade como campos/colunas próprias, sem exigir abrir o detalhe; embalagem ausente mostra traço, nunca quebra o layout.
- CRUD de Categorias e de Templates é restrito a `adm`+ (403 para papéis abaixo); exclusão bloqueada quando há Produto referenciando; edição de Template não força reedição retroativa dos Produtos já cadastrados sob o padrão antigo.
- Categorias e Templates são cópia editável independente por Empresa — toda Empresa nova recebe a mesma lista padrão de partida, mas customizar uma nunca afeta outra. Código de Categoria até 8 caracteres, nome/descrição até 50, limites aplicados no banco.

## Technical Decisions

- Código automático de Produto: tabela dedicada com `empresa_id` como chave primária e contador incrementado atomicamente via `UPDATE ... RETURNING` na mesma transação do `INSERT` do Produto — nunca lazy-init. A primeira linha do contador de cada Empresa nasce na transação de provisionamento da Empresa (junto com a Filial padrão).
- Isolamento por Empresa: Categorias e Templates seguem o padrão geral de `empresa_id NOT NULL` com filtro obrigatório em toda query; exceção é a tabela de contador, que usa `empresa_id` como chave primária própria em vez de coluna de filtro.
- Motor de validação de nome trata o marcador "Genérico" como caso especial (aceita qualquer texto não vazio, sem checar tokens); todo outro template segue validação estrutural por sequência de tokens.
- Código do Fornecedor e EAN-13 são colunas simples no Produto, sem índice de unicidade. Unidade de Medida e Embalagem seguem migração aditiva: coluna nasce nullable, backfill em lote com valor único para Produto legado, só depois a obrigatoriedade passa a valer.
- Exclusão de Categoria/Template bloqueada por qualquer referência de Produto à linha — mesmo princípio já usado para bloquear exclusão de Estoque com resíduo.
- Produto sem saldo em nenhum Estoque continua aparecendo no Catálogo com quantidade total zero — nenhum estado de visibilidade novo é introduzido por esta epic.

## UX & Interaction Patterns

- Card de grade e linha de tabela agrupada do Catálogo já seguem convenção de badge (ícone+texto) e agrupamento por nome/unidade/dimensão; a Story 10.4 estende essas superfícies com as colunas novas, sem layout diferente.
- Campo de embalagem vazio renderiza como traço/vazio, nunca quebra o layout da grade/tabela.
- Exclusão de Categoria/Template usa o componente `ConfirmDialog` reutilizável (`AlertDialog` do shadcn) já padronizado no sistema, nunca `window.confirm()`.

## Cross-Story Dependencies

- Story 10.2 depende do provisionamento de Empresa (Epic 9) criar a linha inicial do contador na mesma transação — sem esse bootstrap atômico, o primeiro cadastro de Produto de uma Empresa nova falha ou colide.
- Story 10.1 depende da lista de Templates (estruturais + Genérico) já existir via seed por Empresa (Epic 9); Story 10.6 permite editar essa lista depois, mas o fallback Genérico precisa estar presente desde o início.
- Story 10.4 consome diretamente os campos persistidos pela Story 10.3 e pelas demais stories do epic — não introduz cálculo próprio além da soma de estoque entre Estoques.
- Stories 10.5 e 10.6 reaproveitam a mesma regra de cópia editável por Empresa e o mesmo princípio de exclusão bloqueada por referência já aplicado a Estoques em epics anteriores.
