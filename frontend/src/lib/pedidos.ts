import { getAccessToken } from '@/lib/session';

/**
 * Cliente HTTP puro (sem Context) da API de leitura de Pedidos — Story 7.3
 * (spec-7-3). Molde de módulo `.ts` puro de `lib/promocao.ts`: só tipos +
 * funções `fetch`, nenhum estado React. `authHeaders()` é a mesma cópia de
 * `carrinho.tsx` (o token da sessão via `getAccessToken`) — este módulo
 * nunca envia `usuarioId`; o escopo (próprios vs. `almoxarife`+) é decidido
 * inteiramente no servidor.
 *
 * `GET /api/pedidos` -> `listarPedidos(status?)` (lista "Meus Pedidos",
 * filtrável por status). `GET /api/pedidos?escopo=todos` ->
 * `listarFilaPedidos(status?)` (Story 7.4, spec-7-4 — a Fila do Almoxarife+:
 * MESMA rota, MESMO endpoint, só o parâmetro de escopo muda; o servidor
 * decide se o chamador realmente enxerga todos os Pedidos ou só os
 * próprios — este módulo nunca checa papel). `GET /api/pedidos/{id}` ->
 * `buscarPedido(id)` (cabeçalho + itens em SNAPSHOT — AD-17, nunca join ao
 * vivo). `POST /api/pedidos/{id}/decisao` -> `decidirPedido(id, aprovar)`
 * (Story 7.5, spec-7-5 — aprovação/rejeição com revalidação de estoque item
 * a item, só `almoxarife`+). `GET /api/pedidos/{id}/recibo` ->
 * `buscarReciboPedidoBlob(id)` (Story 7.6, spec-7-6 — o recibo em PDF de um
 * Pedido já decidido, como `Blob`; SEM lógica de DOM — o componente chamador
 * é quem cria/clica/remove o `<a download>`, molde de `aoExportar`,
 * CatalogoListagem.tsx). Resposta não-ok propaga `body.error?.message` do
 * servidor (ou um fallback).
 */

export type StatusPedido = 'pendente' | 'aprovado' | 'parcialmente_aprovado' | 'rejeitado';

interface PedidoCabecalho {
  id: string;
  usuarioId: string;
  solicitante: string;
  obraCentroCusto: string;
  observacao: string | null;
  status: StatusPedido;
  criadoEm: string;
  // decididoPor/decididoEm (Story 7.5, spec-7-5): auditoria da decisão —
  // presentes no corpo pela mesma projeção do backend, mas NUNCA exibidos
  // nesta story (Never, spec-7-5) — o frontend não os lê em lugar nenhum
  // além destes tipos; a Story 7.6 (recibo, campo "aprovador") consome
  // depois.
  decididoPor: string | null;
  decididoEm: string | null;
}

export interface PedidoResumo extends PedidoCabecalho {
  qtdItens: number;
}

export interface PedidoItem {
  produtoId: string;
  produtoNome: string;
  categoriaNome: string;
  estoqueId: string;
  estoqueNome: string;
  quantidade: number;
  // quantidadeAprovada (Story 7.5, spec-7-5): null enquanto o Pedido
  // permanece `pendente`; a partir da decisão, um valor concreto de 0 até
  // `quantidade` — o quanto de fato foi aprovado/debitado deste item.
  quantidadeAprovada: number | null;
}

export interface PedidoDetalhe extends PedidoCabecalho {
  itens: PedidoItem[];
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_LISTAR =
  'Não foi possível carregar seus pedidos agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_LISTAR_FILA =
  'Não foi possível carregar a fila de pedidos agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_DETALHE =
  'Não foi possível carregar os itens do pedido agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_DECISAO =
  'Não foi possível registrar a decisão agora. Tente novamente em instantes.';
export const MENSAGEM_ERRO_RECIBO =
  'Não foi possível baixar o recibo agora. Tente novamente em instantes.';

export async function listarPedidos(status?: StatusPedido): Promise<PedidoResumo[]> {
  const url = status ? `/api/pedidos?status=${encodeURIComponent(status)}` : '/api/pedidos';
  const res = await fetch(url, { headers: authHeaders() });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_LISTAR);
  }
  const body = (await res.json()) as { pedidos: PedidoResumo[] };
  return body.pedidos ?? [];
}

export async function listarFilaPedidos(status?: StatusPedido): Promise<PedidoResumo[]> {
  const url = status
    ? `/api/pedidos?escopo=todos&status=${encodeURIComponent(status)}`
    : '/api/pedidos?escopo=todos';
  const res = await fetch(url, { headers: authHeaders() });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_LISTAR_FILA);
  }
  const body = (await res.json()) as { pedidos: PedidoResumo[] };
  return body.pedidos ?? [];
}

export async function buscarPedido(id: string): Promise<PedidoDetalhe> {
  const res = await fetch(`/api/pedidos/${encodeURIComponent(id)}`, { headers: authHeaders() });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_DETALHE);
  }
  const body = (await res.json()) as { pedido: PedidoDetalhe };
  return body.pedido;
}

/**
 * Aprova ou rejeita um Pedido `pendente` (Story 7.5, spec-7-5) — só
 * `almoxarife`+ (o servidor revalida o papel a cada requisição via
 * `RequireRole`, nunca cacheado no cliente). A revalidação de estoque item a
 * item acontece inteiramente no servidor, na MESMA chamada de escrita — este
 * módulo nunca faz um GET de preview antes (Design Notes de spec-7-5): o
 * resultado real (`quantidadeAprovada` por item, novo `status`) vem só na
 * resposta deste POST.
 */
export async function decidirPedido(id: string, aprovar: boolean): Promise<PedidoDetalhe> {
  const res = await fetch(`/api/pedidos/${encodeURIComponent(id)}/decisao`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: JSON.stringify({ aprovar }),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_DECISAO);
  }
  const body = (await res.json()) as { pedido: PedidoDetalhe };
  return body.pedido;
}

/**
 * Baixa o recibo em PDF de um Pedido já decidido (Story 7.6, spec-7-6) — só
 * dono OU `almoxarife`+ (mesmo padrão AD-8 de `buscarPedido`, decidido
 * inteiramente no servidor). Devolve o `Blob` puro; SEM lógica de DOM neste
 * módulo — quem cria/clica/remove o `<a download>` é o componente chamador
 * (molde de `aoExportar`, CatalogoListagem.tsx). Resposta não-ok (`404`
 * Pedido alheio/inexistente, `409` Pedido ainda não decidido) propaga
 * `body.error?.message` do servidor (ou `MENSAGEM_ERRO_RECIBO`).
 */
export async function buscarReciboPedidoBlob(id: string): Promise<Blob> {
  const res = await fetch(`/api/pedidos/${encodeURIComponent(id)}/recibo`, {
    headers: authHeaders(),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_RECIBO);
  }
  return res.blob();
}
