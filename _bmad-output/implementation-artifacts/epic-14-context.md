# Epic 14 Context: Dupla autenticação por Empresa

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Hoje a exigência de MFA (TOTP) é fixa para todo `gestor`/`adm` em qualquer Empresa. A pedido dos sócios e de clientes, este epic transforma essa exigência numa escolha de cada Empresa. O padrão é "Não", inclusive para as Empresas já existentes, então ninguém é bloqueado no deploy. O epic cobre o gate condicional no servidor e o espelho no frontend. Cobre também a pergunta no cadastro da Empresa, com o Treinamento herdando a escolha, e a alteração posterior pelo `adm`, com aviso e auditoria. Por fim, cobre a recuperação de acesso para quem perdeu o celular, já que o MFA opcional não pode virar um bloqueio permanente.

## Stories

- Story 14.1: A Empresa passa a definir se exige MFA (gate condicional)
- Story 14.2: Cadastro da Empresa pergunta se exige MFA; Treinamento herda
- Story 14.3: O adm altera a exigência de MFA, com aviso e auditoria
- Story 14.4: Recuperação: reset de MFA por adm e desligamento pela própria conta

## Requirements & Constraints

- **Empresa que exige:** `gestor`/`adm` autenticado por senha e sem MFA fica sem acesso até configurar. O servidor recusa as rotas restritas a esses papéis, e a interface libera só Configurações → Segurança e o logout.
- **Empresa que não exige:** todos os papéis operam normalmente. Configurar o MFA continua opcional para qualquer papel.
- A exigência vale só para `gestor`/`adm`. `usuario`/`almoxarife` nunca são bloqueados por ela.
- **O segundo fator ligado pela conta é sempre respeitado:** se a conta tem MFA, o código é pedido no login, com ou sem exigência da Empresa. Desligar a exigência nunca desliga o MFA de ninguém, só remove a obrigação.
- **Login via SSO:** o gate nunca dispara, porque o realm Keycloak corporativo já impõe MFA.
- **Dono da Plataforma:** o MFA dele continua obrigatório, sem exceção, e não lê o flag de nenhuma Empresa.
- **Promoção a `gestor`/`adm`:** o promovido só é obrigado a configurar MFA se a Empresa exigir.
- **Redefinição de senha por e-mail:** não altera nem contorna o MFA.
- **Cadastro da Empresa:** a pergunta "Esta Empresa exige dupla autenticação?" já vem com **Não** pré-selecionado. Se o campo não vier no corpo, o servidor assume `false` e não falha. A listagem do Dono da Plataforma mostra a escolha de cada Empresa, que é só metadado administrativo.
- **Treinamento:** herda a escolha **na criação**. Depois disso, as duas Empresas são independentes, sem propagação.
- **Ligar depois:** antes de confirmar, a tela avisa quantos `gestor`/`adm` da Empresa ainda não têm MFA.
- **Gravar o mesmo valor de novo é idempotente:** nada muda e nenhuma linha de auditoria é criada.
- **Recuperação:**
  - (a) O `adm` reseta o MFA de uma conta da própria Empresa com rank menor que o dele.
  - (b) Qualquer conta desliga o próprio MFA informando a senha atual e o código TOTP vigente. Senha ou código errado conta para o bloqueio por tentativas (5 tentativas → 15 minutos).
  - Desligar o próprio MFA é recusado com 409 para `gestor`/`adm` numa Empresa que exige.
- **Fora do escopo:** MFA para `usuario`/`almoxarife`, segundo fator diferente de TOTP e códigos de recuperação impressos.

## Technical Decisions

- **Dado:** `empresas.mfa_obrigatorio BOOLEAN NOT NULL DEFAULT false`, em migração aditiva. `services.Empresa` ganha `MFAObrigatorio`, e `colunasEmpresa` passa a ler a coluna.
- **Gate único em `RequireRole` (`middleware/roles.go`):** o gate emite `403 MFA_SETUP_REQUIRED` somente quando as quatro condições valem juntas:
  - rank do papel mínimo da rota >= `gestor`;
  - `usuario.Origem == "senha"`;
  - `!usuario.MFAHabilitado`;
  - `empresa.MFAObrigatorio`.

  O gate nunca é reimplementado em handler ou service.
- **Sem cache:** a Empresa vem de `EmpresaDaRequisicao(ctx)`, resolvida por slug a cada requisição. Por isso uma mudança do flag vale já na próxima requisição, sem novo login.
- **Frontend só espelha:** `GET /api/auth/me` devolve `empresa.mfaObrigatorio`. `App.tsx` (bloqueio de navegação) e `ConfiguracoesPage` (rótulo "obrigatório" ou "opcional") usam `origem==='senha' && rank>=gestor && !mfaHabilitado && empresa.mfaObrigatorio`. O servidor continua sendo a autoridade.
- **Quem altera:**
  - O `adm` usa uma rota própria atrás de `RequireRole(adm)`, que já sujeita o próprio adm ao gate.
  - O Dono da Plataforma define o valor só no cadastro: campo `mfa_obrigatorio` em `POST /api/plataforma/empresas`, via `NovaEmpresaInput`. `CriarEmpresaComTreinamento` copia o valor para o Treinamento.
  - Quem não é Dono da Plataforma recebe 403.
- **Reset por `adm`:** respeita a regra de rank (o ator precisa de rank maior que o do alvo). Zera `mfa_habilitado`, `mfa_secret` e `mfa_ultimo_passo_usado` e revoga as sessões da conta-alvo.
- **Erros:** conta de outra Empresa recebe 404 sem revelar se ela existe (escopo por `empresa_id`). Falta de permissão recebe 403.
- **`auditoria_seguranca`:** tabela append-only com as colunas:
  - `id`, `empresa_id`, `ator_id`;
  - `alvo_id` (nulável);
  - `acao`: enum `exigencia_alterada | mfa_resetado | mfa_desligado`;
  - `detalhe` (jsonb; para `exigencia_alterada`, guarda o valor anterior e o novo);
  - `criado_em`.

  A tabela é escopada por `empresa_id`, não tem rota de edição ou exclusão e é consultável pelo `adm` da própria Empresa.
- **Arquivos-alvo previstos:** `middleware/roles.go`, `services/auth.go`, `handlers/auth_mfa.go`.
- **Biblioteca TOTP:** reaproveitar a já em uso. A spine não vincula uma biblioteca nova.

## UX & Interaction Patterns

- **Configurações → Segurança:** mostra se o MFA é "obrigatório" ou "opcional" para a conta.
  - Para o `adm`, há a seção "Dupla autenticação da Empresa", com a escolha atual, o controle para alterá-la, a confirmação com o aviso de contas sem MFA e o histórico de auditoria. Papéis abaixo de `adm` não veem o controle.
  - O item "Desligar meu MFA" fica aqui e pede a senha atual e o código TOTP.
- **Gestão de Contas:** ação "Resetar MFA", disponível ao `adm` para contas de rank menor.
- **`EmpresasPage` (Dono da Plataforma):** a pergunta de MFA no formulário de cadastro e a coluna com a escolha na listagem.
- **Bloqueio por MFA exigido:** redireciona para Configurações → Segurança. A navegação fica bloqueada, não escondida. No mobile, Segurança fica sob "Mais".

## Cross-Story Dependencies

- A 14.1 é base de todas: coluna, gate, `/api/auth/me` e espelho no frontend.
- A 14.2 e a 14.3 dependem da coluna e do tipo `Empresa` da 14.1.
- A 14.3 cria `auditoria_seguranca`, que a 14.4 reutiliza (`mfa_resetado`, `mfa_desligado`). Se a 14.4 for feita antes, precisa criar a tabela.
- Depende de peças de epics anteriores: fluxo de MFA/TOTP e login com código (Story 1.11), `RequireRole` com rank, resolução de Empresa por slug, `CriarEmpresaComTreinamento`, `EmpresasPage` e Gestão de Contas.
