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
