# Epic 9 Context: Multi-Empresa e Plataforma

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Transformar o stockflow de uma instalação única da Ferreira Costa em uma plataforma multi-cliente com isolamento total de dados entre Empresas. A Empresa passa a ser a unidade de isolamento: todo Produto, Estoque, Movimentação, Pedido, Categoria, Log de Acesso e conta de Usuário pertence a exatamente uma Empresa, e nenhuma consulta, agregação ou exportação cruza essa fronteira — para nenhum papel. Sobre essa fundação, um papel novo e cross-Empresa ("Dono da Plataforma") cria Empresas em nível de metadado, cada Empresa nova nasce com um Ambiente de Treinamento isolado, o vínculo de uma pessoa a uma Empresa passa a exigir convite nominal, e a Ferreira Costa migra para o novo modelo virando a primeira Empresa real, sem perder histórico. O rigor aqui é máximo: vazamento entre Empresas é falha de produto, não bug cosmético.

## Stories

- Story 9.1: Fundação Multi-Empresa — schema e isolamento por Empresa
- Story 9.2: Dono da Plataforma cria Empresas com Ambiente de Treinamento automático
- Story 9.3: Convite nominal de acesso a uma Empresa
- Story 9.4: Migração da Ferreira Costa para o modelo multi-Empresa

## Requirements & Constraints

- **Isolamento total, testável.** Nenhuma área existente pode vazar dado entre Empresas: Catálogo, Estoques, Movimentações, Pedidos, Log de Acesso, Normalização (Inconsistências e Duplicatas) e Gestão de Contas/Promoção de Papel. O critério de aceitação é teste automatizado que cria dados em duas Empresas e prova que uma nunca aparece na consulta da outra.
- **Duplicatas exige barreira explícita.** A mesclagem é destrutiva e irreversível; detectar, agrupar e mesclar considera SOMENTE Produtos da mesma Empresa do ator — inclusive entre uma Empresa real e seu Ambiente de Treinamento.
- **Unicidades deixam de ser globais.** E-mail de Usuário e o papel `adm` passam a ser únicos **por Empresa** (cada Empresa tem exatamente um `adm` ativo). Todo índice/constraint hoje global precisa ser reescopado. CNPJ, ao contrário, é único em toda a plataforma (409 em duplicata) e validado no formato (14 dígitos + verificadores).
- **Dono da Plataforma vê só metadado:** Nome Fantasia, Razão Social, CNPJ, endereço, status, `adm` responsável, data de criação. Nunca Catálogo, Estoque, Pedido, Log de Acesso ou Duplicatas de nenhuma Empresa. Exige MFA obrigatório, sem exceção.
- **Convite é nominal e de uso único**, vinculado a um e-mail específico, com expiração e revogação; o autocadastro só é aceito se o e-mail do formulário bater exatamente com o do convite, mesmo com token válido. Papel da conta criada é sempre `usuario`. Não existe link genérico reutilizável nem vínculo por domínio de e-mail.
- **Login nunca pede a Empresa** — ela é resolvida pelo contexto de acesso, jamais por dropdown, mesmo que o mesmo e-mail exista em Empresas diferentes.
- **Desativar Empresa não apaga dados:** impede login de qualquer conta vinculada, preservando o conteúdo.
- Um Usuário pertence a exatamente uma Empresa; trocar exige conta nova com novo convite.
- **Categorias passam a ser cópia editável por Empresa** a partir da mesma lista padrão; customizar em uma não afeta as demais.
- **Migração de produção é sempre disparada por pessoa**, nunca de forma autônoma por agente de IA, mesmo que o código tenha sido escrito sob o processo de agentes.
- O isolamento não pode degradar o tempo de resposta do catálogo abaixo do SLA já definido.
- **Fora de escopo (não inventar):** negócios entre Empresas (catálogo/estoque/pedido compartilhados), cadastro self-service de Empresa, planos comerciais/billing, white-label, reset periódico do Ambiente de Treinamento, extensão do SSO Keycloak a outras Empresas.

## Technical Decisions

- **Empresa resolvida uma única vez no middleware.** A URL carrega um path prefix com o slug (`/e/{slug}/...`); o middleware resolve `empresa_id` a partir do slug e o injeta no contexto da requisição junto com o papel já resolvido — mesmo padrão do middleware de autorização existente. Nenhum service re-deriva a Empresa nem aceita `empresa_id` de body, query ou header. Login e todo endpoint de domínio vivem sob esse prefixo. Subdomínio por Empresa foi descartado deliberadamente (exigiria DNS/certificado wildcard, incompatível com o host único atual).
- **`empresa_id` em toda tabela de domínio:** `produtos`, `estoques`, `movimentacoes`, `pedidos`, `pedido_itens`, `categorias`, `logs_acesso`, `solicitacoes_promocao`, `mesclagens_duplicatas`, `mesclagem_produtos_removidos`, `importacoes`, `nomenclatura_templates`, `usuarios`. Toda query de service que lê ou escreve uma delas filtra por `empresa_id` do contexto — inclusive agregações e relatórios.
- **Tabela `empresas`:** id, nome_fantasia, razao_social, cnpj, endereço completo (logradouro/número/complemento, bairro, cidade, CEP, UF), slug, status, `empresa_origem_id` nullable.
- **Dono da Plataforma vive em tabela própria (`donos_plataforma`), disjunta de `usuarios`** — id, e-mail, senha_hash, mfa_habilitado, mfa_secret (MFA nunca opcional). Rota de login própria, fora do prefixo `/e/{slug}`. Nunca estender a escala de rank de papéis intra-Empresa com um quinto nível: o papel é ortogonal, não superior. Formato de sessão reaproveita o JWT curto + refresh rotativo existente — só o sujeito do token e a rota de emissão mudam. Primeiro registro é bootstrap por CLI dedicado, nunca por rota HTTP; rotas de gestão de Empresas exigem sessão de `donos_plataforma` e jamais aceitam elevação a partir de uma sessão de `usuarios`.
- **Tabela `convites_empresa`:** id, empresa_id, email, token, expira_em, usado_em, criado_por. Separada do mecanismo de tokens de ação existente, porque não há `usuario_id` no momento da criação. `usado_em` é marcado atomicamente na primeira criação de conta bem-sucedida.
- **Ambiente de Treinamento é uma Empresa comum.** `empresa_origem_id` NULL para Empresa real, preenchido com o id da Empresa real quando a linha é um Ambiente de Treinamento. Isolamento, resolução de slug e papéis tratam-na como uma segunda Empresa cliente qualquer — nenhum `if eh_treinamento` em service algum, nenhum segundo mecanismo de isolamento. Nasce com um conjunto pequeno de Produtos/Estoques de exemplo, nunca copiados de dados reais.
- **Migração aditiva em duas fases, sem downtime obrigatório:** (1) `empresa_id` nasce nullable e um backfill em lote — resumível e idempotente, reexecutar não duplica nem perde linha — atribui a Empresa "Ferreira Costa" a toda linha existente; (2) só após backfill 100% confirmado, `SET NOT NULL` + índice. Nunca um `ALTER ... NOT NULL` direto sobre produção viva. Tamanho de lote é decisão de implementação.
- Convenções gerais seguem valendo: UUID v4, `timestamptz` UTC no banco / ISO 8601 na API, e-mail normalizado para minúsculas antes da escrita, envelope de erro fixo com o vocabulário de códigos existente (`FORBIDDEN`, `VALIDATION_ERROR`, `CONFLICT`, ...), logging estruturado com `log/slog`.
- Rate limit de emissão de convites e de tentativas de login do Dono da Plataforma não está fechado — resolver junto do item já diferido de bloqueio por força bruta.

## UX & Interaction Patterns

- Os artefatos de UX são anteriores ao pivô multi-Empresa e não descrevem estas telas; implemente contra os critérios de aceitação usando os padrões já estabelecidos (shell com rail/bottom nav, `Card`/`Table`/`Skeleton`/`Toast`, `AlertDialog` para toda ação destrutiva, toast `aria-live="polite"`, alvo de toque de 48px, viewport mínimo de 360px).
- A área "Empresas" é superfície administrativa exclusiva do Dono da Plataforma, fora da navegação de qualquer Empresa, e nunca exibe conteúdo operacional. Desativar uma Empresa passa por `AlertDialog` com token `destructive`.
- O Ambiente de Treinamento precisa de distinção visual persistente e difícil de ignorar (a URL com slug próprio já ajuda; banner discreto sozinho é insuficiente) — o risco é confundir treinamento com produção. O comportamento dentro dele é idêntico ao de produção, inclusive recusas; só os dados são de mentira.

## Cross-Story Dependencies

- **Story 9.1 é bloqueante para 9.2, 9.3 e 9.4** — schema com `empresa_id`, resolução de Empresa no middleware e filtro em todos os services são a fundação de tudo mais.
- **Story 9.4 depende de 9.2**, reaproveitando o provisionamento do Ambiente de Treinamento em vez de duplicá-lo; roda depois que o schema multi-Empresa está em produção e antes de qualquer segunda Empresa real existir.
- **Story 9.3 depende do autocadastro, login, MFA e gestão de papéis do Epic 1**, que passam a exigir convite e contexto de Empresa.
- **Story 9.1 toca retroativamente todos os épicos anteriores** — Estoques (2), Produtos/Importação (3), Catálogo (4), Movimentações (5), Normalização/Duplicatas (6), Pedidos (7), Log de Acesso e Gestão de Contas (1) e exportações LGPD (8): cada um precisa das queries reescopadas, não só as rotas novas.
- A conta `adm` global hoje existente e a identidade do Dono da Plataforma são a mesma pessoa, mas nunca a mesma credencial — 9.2 e 9.4 devem preservar essa separação estrutural.
