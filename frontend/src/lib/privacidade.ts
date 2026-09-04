import { getAccessToken } from '@/lib/session';

/**
 * Cliente HTTP puro (sem Context) da exportação dos próprios dados pessoais
 * — Story 8.1 (spec-8-1). Molde de módulo `.ts` puro de `lib/pedidos.ts`:
 * só tipos + funções `fetch`, nenhum estado React. `authHeaders()` é a
 * mesma cópia usada em `lib/pedidos.ts`/`carrinho.tsx` (o token da sessão
 * via `getAccessToken`) — este módulo nunca envia `usuarioId`; o escopo
 * (sempre os próprios dados) é decidido inteiramente no servidor.
 *
 * `GET /api/usuarios/me/exportar-dados` -> `baixarMeusDadosBlob()`. Devolve
 * o `Blob` puro; SEM lógica de DOM neste módulo — quem cria/clica/remove o
 * `<a download>` é o componente chamador (molde de `buscarReciboPedidoBlob`,
 * `lib/pedidos.ts`). Resposta não-ok propaga `body.error?.message` do
 * servidor (ou MENSAGEM_ERRO_EXPORTAR).
 */

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

export const MENSAGEM_ERRO_EXPORTAR =
  'Não foi possível baixar seus dados agora. Tente novamente em instantes.';

export async function baixarMeusDadosBlob(): Promise<Blob> {
  const res = await fetch('/api/usuarios/me/exportar-dados', {
    headers: authHeaders(),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_EXPORTAR);
  }
  return res.blob();
}

/**
 * Exclusão e anonimização de dados pessoais por Adm — Story 8.2 (spec-8-2).
 * Exclusão baseada em solicitação, nunca self-service: a rota `me` só
 * REGISTRA a solicitação da própria conta; só um `adm` lista e processa
 * (anonimiza). Nenhuma função aqui envia um alvo — o escopo é decidido
 * inteiramente no servidor (a conta da sessão para `solicitarExclusaoConta`;
 * `solicitacoes_exclusao_conta.solicitante_id` para o processamento).
 */

/** Item da fila de solicitações de exclusão pendentes (lado Adm). */
export interface SolicitacaoExclusao {
  id: string;
  nome: string;
  email: string;
  papel: string;
  criadoEm: string;
}

export const MENSAGEM_ERRO_SOLICITAR_EXCLUSAO =
  'Não foi possível registrar a solicitação de exclusão agora. Tente novamente em instantes.';
export const MENSAGEM_ERRO_LISTAR_EXCLUSAO =
  'Não foi possível carregar as solicitações de exclusão. Recarregue a página.';
export const MENSAGEM_ERRO_PROCESSAR_EXCLUSAO =
  'Não foi possível processar a exclusão agora. Tente novamente em instantes.';

async function mensagemDeErro(res: Response, fallback: string): Promise<string> {
  const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
  return body.error?.message ?? fallback;
}

/** `POST /api/usuarios/me/solicitacao-exclusao` — registra a solicitação da própria conta. */
export async function solicitarExclusaoConta(): Promise<void> {
  const res = await fetch('/api/usuarios/me/solicitacao-exclusao', {
    method: 'POST',
    headers: authHeaders(),
  });
  if (!res.ok) {
    throw new Error(await mensagemDeErro(res, MENSAGEM_ERRO_SOLICITAR_EXCLUSAO));
  }
}

/** `GET /api/solicitacoes-exclusao` — fila de pendentes (só `adm`). */
export async function listarSolicitacoesExclusao(): Promise<SolicitacaoExclusao[]> {
  const res = await fetch('/api/solicitacoes-exclusao', { headers: authHeaders() });
  if (!res.ok) {
    throw new Error(await mensagemDeErro(res, MENSAGEM_ERRO_LISTAR_EXCLUSAO));
  }
  const body = (await res.json()) as { solicitacoes?: SolicitacaoExclusao[] };
  return body.solicitacoes ?? [];
}

/**
 * `POST /api/solicitacoes-exclusao/{id}/processamento` — anonimiza a conta
 * alvo da solicitação. Resposta não-ok propaga `body.error?.message`: a
 * mensagem do guard do último administrador vem do servidor.
 */
export async function processarExclusaoConta(id: string): Promise<void> {
  const res = await fetch(`/api/solicitacoes-exclusao/${id}/processamento`, {
    method: 'POST',
    headers: authHeaders(),
  });
  if (!res.ok) {
    throw new Error(await mensagemDeErro(res, MENSAGEM_ERRO_PROCESSAR_EXCLUSAO));
  }
}
