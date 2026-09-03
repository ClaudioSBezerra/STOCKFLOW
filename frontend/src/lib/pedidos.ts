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
 * filtrável por status). `GET /api/pedidos/{id}` -> `buscarPedido(id)`
 * (cabeçalho + itens em SNAPSHOT — AD-17, nunca join ao vivo). Resposta
 * não-ok propaga `body.error?.message` do servidor (ou um fallback).
 */

export type StatusPedido = 'pendente' | 'aprovado' | 'rejeitado';

interface PedidoCabecalho {
  id: string;
  usuarioId: string;
  solicitante: string;
  obraCentroCusto: string;
  observacao: string | null;
  status: StatusPedido;
  criadoEm: string;
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
const MENSAGEM_ERRO_DETALHE =
  'Não foi possível carregar os itens do pedido agora. Tente novamente em instantes.';

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

export async function buscarPedido(id: string): Promise<PedidoDetalhe> {
  const res = await fetch(`/api/pedidos/${encodeURIComponent(id)}`, { headers: authHeaders() });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_DETALHE);
  }
  const body = (await res.json()) as { pedido: PedidoDetalhe };
  return body.pedido;
}
