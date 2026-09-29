# Epic 18 Context: Período de teste de 14 dias, com bloqueio automático e liberação manual

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Antes de existir cobrança automática (integração futura com a Hotmart), toda Empresa nova precisa de um limite claro de uso gratuito. Toda Empresa real criada a partir de agora ganha 14 dias corridos de teste, contados da criação; o Ambiente de Treinamento dela herda o mesmo prazo. Esgotado o prazo, a Empresa (e o Treinamento) fica bloqueada por completo — nenhuma rota de negócio responde, e a pessoa vê uma tela explicando o vencimento e como assinar. Só o Dono da Plataforma estende ou isenta uma Empresa, pela tela de gestão de Empresas que já existe. Empresas já existentes na plataforma (Ferreira Costa, FBTECHIA) não são afetadas.

## Stories

- Story 18.1: Toda Empresa nova ganha 14 dias de teste, herdados pelo Treinamento
- Story 18.2: Empresa com o prazo vencido fica bloqueada por completo
- Story 18.3: Dono da Plataforma estende ou isenta o prazo de uma Empresa

## Requirements & Constraints

- Toda Empresa real criada a partir de agora nasce com prazo de teste de 14 dias corridos contados da criação; o Ambiente de Treinamento dela herda o mesmo prazo.
- Empresas já existentes na plataforma na data desta versão (Ferreira Costa, FBTECHIA) não são afetadas — continuam sem prazo, sem risco de bloqueio retroativo. Qualquer outro caminho de provisionamento fora da tela do Dono (ex. migração/adoção de Empresa fundadora) também nasce sem prazo (isento).
- Esgotado o prazo, nenhuma rota de negócio da Empresa nem do Treinamento responde — inclusive para quem já estava logado, sem carência até a sessão expirar sozinha. A pessoa vê uma tela dedicada explicando que o teste acabou e como assinar/falar com a plataforma, nunca um erro genérico.
- Só o Dono da Plataforma pode estender o prazo (por N dias) ou marcar a Empresa como isenta (sem prazo, nunca bloqueia), pela tela de gestão de Empresas — é a válvula de escape manual até existir cobrança automática.
- Estender ou isentar o Treinamento é uma ação independente da Empresa real — o Dono decide os dois separadamente se precisar; nada mantém os dois prazos sincronizados depois da criação.
- Fora de escopo: cobrança automática/integração de pagamento (Hotmart ou outro meio, fica para Epic futuro); aviso por e-mail antes do vencimento; qualquer bloqueio parcial ou somente-leitura — o bloqueio é sempre total.
- A tela "Empresas" do Dono da Plataforma passa a mostrar, por linha, o prazo de teste: dias restantes, "Vencido há N dias", ou "Sem prazo" (isenta).

## Technical Decisions

- Nova coluna `empresas.trial_termina_em` (`TIMESTAMPTZ NULL`) — migração aditiva; `NULL` em toda Empresa já existente e em qualquer Empresa provisionada fora do fluxo comercial (ex. `cmd/migrar-multi-empresa`, que adota a Empresa fundadora). `NULL` significa isenta/sem prazo.
- Só `CriarEmpresaComTreinamento` grava prazo: escreve `trial_termina_em = now() + 14 dias` na Empresa real antes de provisioná-la; o provisionamento do Treinamento herda o valor JÁ GRAVADO na Empresa real (nunca recalcula) — mesmo padrão usado para a herança do flag de MFA obrigatório (Epic 14): herda só no instante da criação, nunca propaga depois. Os dois prazos ficam independentes a partir daí.
- Bloqueio centralizado no único ponto que resolve Empresa por slug (o mesmo middleware que traduz slug → Empresa a cada requisição, sem cache): checa `trial_termina_em` logo após resolver a Empresa; se está no passado, responde `402 PAYMENT_REQUIRED` com código `TRIAL_EXPIRADO` — nunca o `404` usado para slug inexistente ou Empresa desativada, porque a pessoa precisa saber que a Empresa existe e como resolver. Isso barra toda rota de negócio sob o prefixo da Empresa, inclusive login e sessão já aberta, no primeiro request seguinte ao vencimento — sem checagem em background nem job/cron separado.
- Login pela raiz do domínio não duplica a checagem: só resolve a conta e redireciona para a Empresa; o primeiro request de sessão ali já esbarra no mesmo gate.
- Frontend: o cliente HTTP intercepta `TRIAL_EXPIRADO` globalmente (mesmo nível de tratamento de sessão revogada) e troca a tela normal por um aviso dedicado ("seu teste acabou, veja como assinar"), nunca o toast de erro genérico usado para outras falhas.
- Liberação manual: rota própria restrita ao papel Dono da Plataforma, aceitando `{"acao": "estender", "dias": N}` (soma N dias a partir de agora, não a partir do prazo antigo) ou `{"acao": "isentar"}` (grava `NULL`); qualquer outro papel recebe `403`. Sem efeito automático sobre o Treinamento da mesma Empresa — o Dono repete a ação separadamente nele se quiser o mesmo efeito.
- Empresa com `trial_termina_em` `NULL` (isenta) ou no futuro: nada muda, comportamento idêntico ao de hoje.

## Cross-Story Dependencies

- Story 18.2 e 18.3 dependem da coluna e da lógica de herança criadas na Story 18.1.
- Story 18.3 depende do bloqueio da Story 18.2 existir para que "estender"/"isentar" tenham efeito observável (Empresa bloqueada volta a responder normalmente na próxima requisição após a ação).
- Reaproveita a tela "Empresas" e o papel Dono da Plataforma já existentes (Epic 9 — Multi-Empresa e Plataforma) e o único ponto de resolução de Empresa por slug já existente nesse mesmo épico — nenhuma rota nova de resolução é criada.
- Segue o mesmo padrão de herança-só-na-criação estabelecido pelo Epic 14 (MFA obrigatório por Empresa) para a relação Empresa real → Ambiente de Treinamento.
