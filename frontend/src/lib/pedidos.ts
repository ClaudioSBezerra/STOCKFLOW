import { apiUrl, authHeaders } from '@/lib/api';

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

/**
 * Tipo do Pedido (Story 20.1, Epic 20): `retirada` (Epic 7, já existente) |
 * `transferencia` (Story 20.1) | `baixa` (Story 20.2, ainda sem nenhuma tela
 * que o produza). `TipoPedidoBadge` usa este campo para distinguir o Pedido
 * de Transferência do Pedido de retirada comum na Fila/Meus Pedidos.
 */
export type TipoPedido = 'retirada' | 'transferencia' | 'baixa';

interface PedidoCabecalho {
  id: string;
  usuarioId: string;
  solicitante: string;
  obraCentroCusto: string;
  // tipo (Story 20.1): opcional só para tolerar mocks/fixtures de teste
  // anteriores a esta story, que ainda não incluem o campo — ausente é
  // tratado como 'retirada' (o default do servidor) em todo lugar que o lê.
  tipo?: TipoPedido;
  // Nome do Centro de Custo cadastrado escolhido no envio (Story 12.3 +
  // decisão do code review); null/ausente quando o Pedido não escolheu um.
  centroCusto?: string | null;
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
  // preNotaId/docnum (Story 20.4/20.5): preenchidos só quando o Pedido foi
  // aprovado em lote e ganhou uma Pré-Nota; null/ausente caso contrário
  // (opcionais para tolerar fixtures anteriores à Story 20.4).
  preNotaId?: string | null;
  docnum?: number | null;
}

export interface PedidoResumo extends PedidoCabecalho {
  qtdItens: number;
}

/**
 * Só o cabeçalho do Pedido, sem itens nem contagem — a projeção devolvida
 * por POST /api/pedidos/transferencia (Story 20.1, molde de
 * `services.Pedido` no backend).
 */
export type Pedido = PedidoCabecalho;

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
  // inativo (Story 16.2): o Produto foi inativado depois do envio.
  inativo?: boolean;
  // produtoCodigo (feedback Ferreira Costa, direção, 2026-10-20): lido ao
  // vivo, como inativo acima — null enquanto o Produto não tiver código.
  produtoCodigo: string | null;
}

export interface PedidoDetalhe extends PedidoCabecalho {
  itens: PedidoItem[];
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
  const res = await fetch(apiUrl(url), { headers: authHeaders() });
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
  const res = await fetch(apiUrl(url), { headers: authHeaders() });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_LISTAR_FILA);
  }
  const body = (await res.json()) as { pedidos: PedidoResumo[] };
  return body.pedidos ?? [];
}

const MENSAGEM_ERRO_TRANSFERENCIA =
  'Não foi possível enviar o pedido de transferência agora. Tente novamente em instantes.';

/**
 * Corpo de POST /api/pedidos/transferencia (Story 20.1, Epic 20, spec-20-1).
 * SEM `solicitante` (requisito do usuário, pós-revisão, mesmo molde de
 * SubmeterPedidoBaixaInput: "Solicitante é o usuário logado, não precisa
 * pedir") — o servidor resolve o nome da sessão autenticada, nunca aceito do
 * corpo. `obraCentroCusto` é OPCIONAL (requisito do usuário, pós-revisão,
 * mesmo molde de SubmeterPedidoBaixaInput).
 */
export interface SubmeterPedidoTransferenciaInput {
  obraCentroCusto?: string;
  observacao?: string;
  produtoId: string;
  estoqueOrigemId: string;
  estoqueDestinoId: string;
  quantidade: number;
}

/**
 * Envia um Pedido de Transferência entre Filiais (Story 20.1, Epic 20,
 * spec-20-1) — 1 item só, enviado DIRETO (sem passar pelo Carrinho). Reserva
 * dura só da ORIGEM; nunca debita/credita saldo físico (isso é a Story
 * 20.3). `solicitante` é sempre o usuário logado, resolvido pelo servidor —
 * este módulo nunca o envia. Resposta não-ok propaga `body.error?.message` do
 * servidor (400 VALIDATION_ERROR para campos vazios/origem==destino/destino
 * inválido, 409 CONFLICT para disponibilidade insuficiente, 409
 * PRODUTO_INATIVO para Produto inativo) ou o fallback acima.
 */
export async function submeterPedidoTransferencia(
  input: SubmeterPedidoTransferenciaInput,
): Promise<Pedido> {
  const res = await fetch(apiUrl('/api/pedidos/transferencia'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: JSON.stringify(input),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_TRANSFERENCIA);
  }
  const body = (await res.json()) as { pedido: Pedido };
  return body.pedido;
}

const MENSAGEM_ERRO_BAIXA =
  'Não foi possível enviar o pedido de baixa agora. Tente novamente em instantes.';

/**
 * Corpo de POST /api/pedidos/baixa (Story 20.2, Epic 20, spec-20-2). SEM
 * `solicitante` (requisito do usuário, pós-revisão: "Solicitante é o
 * usuário logado, não precisa pedir") — o servidor resolve o nome da sessão
 * autenticada, nunca aceito do corpo.
 */
export interface SubmeterPedidoBaixaInput {
  obraCentroCusto?: string;
  observacao?: string;
  produtoId: string;
  estoqueId: string;
  quantidade: number;
}

/**
 * Envia um Pedido de Baixa de Estoque (Story 20.2, Epic 20, spec-20-2) — 1
 * item só, enviado DIRETO (sem passar pelo Carrinho). Reserva dura do par
 * (Produto, Estoque); nunca debita saldo físico (RegistrarBaixa,
 * movimentacoes.go, continua a ação direta sem aprovação). `solicitante` é
 * sempre o usuário logado, resolvido pelo servidor — este módulo nunca o
 * envia. Resposta não-ok propaga `body.error?.message` do servidor (400
 * VALIDATION_ERROR para campos vazios, 409 CONFLICT para disponibilidade
 * insuficiente, 409 PRODUTO_INATIVO para Produto inativo) ou o fallback
 * acima.
 */
export async function submeterPedidoBaixa(input: SubmeterPedidoBaixaInput): Promise<Pedido> {
  // Revisão pós-Story 20.2: `fetch()` pode rejeitar sozinho (rede
  // offline/DNS/etc.) ANTES de qualquer resposta existir — sem este
  // try/catch, esse erro cru (nunca uma mensagem amigável) vazaria direto
  // pro chamador. Mesmo cuidado no corpo de sucesso logo abaixo: um `res.ok`
  // com corpo que não é JSON válido também nunca deve vazar um erro cru.
  let res: Response;
  try {
    res = await fetch(apiUrl('/api/pedidos/baixa'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...authHeaders() },
      body: JSON.stringify(input),
    });
  } catch {
    throw new Error(MENSAGEM_ERRO_BAIXA);
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_BAIXA);
  }
  const body = (await res.json().catch(() => null)) as { pedido: Pedido } | null;
  if (!body) {
    throw new Error(MENSAGEM_ERRO_BAIXA);
  }
  return body.pedido;
}

export async function buscarPedido(id: string): Promise<PedidoDetalhe> {
  const res = await fetch(apiUrl(`/api/pedidos/${encodeURIComponent(id)}`), { headers: authHeaders() });
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
  const res = await fetch(apiUrl(`/api/pedidos/${encodeURIComponent(id)}/decisao`), {
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

const MENSAGEM_ERRO_DECISAO_LOTE =
  'Não foi possível registrar a decisão do lote agora. Tente novamente em instantes.';

/**
 * Aprova ou rejeita, numa única chamada, TODOS os Pedidos de Transferência
 * e/ou Baixa de `pedidoIds` (Story 20.3, Epic 20, spec-20-3) — caminho
 * PARALELO a `decidirPedido`, só para `gestor`/`adm` (o servidor revalida o
 * papel a cada requisição via `RequireRole`, nunca cacheado no cliente).
 * Devolve `PedidoDetalhe[]` na mesma ordem de `pedidoIds` — cada um já com o
 * status final (decidido agora OU já decidido antes por outra requisição
 * concorrente, reportado em vez de abortar o lote). Resposta não-ok propaga
 * `body.error?.message` do servidor (400 VALIDATION_ERROR para lista vazia
 * ou com algum id de Pedido de retirada misturado, 403 para papel abaixo de
 * `gestor`) ou o fallback acima.
 */
export async function decidirLotePedidos(
  pedidoIds: string[],
  aprovar: boolean,
): Promise<PedidoDetalhe[]> {
  const res = await fetch(apiUrl('/api/pedidos/decisao-lote'), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: JSON.stringify({ pedidoIds, aprovar }),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_DECISAO_LOTE);
  }
  const body = (await res.json()) as { pedidos: PedidoDetalhe[] };
  return body.pedidos ?? [];
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
  const res = await fetch(apiUrl(`/api/pedidos/${encodeURIComponent(id)}/recibo`), {
    headers: authHeaders(),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_RECIBO);
  }
  return res.blob();
}

export const MENSAGEM_ERRO_PRE_NOTA =
  'Não foi possível baixar a Pré-Nota agora. Tente novamente em instantes.';

/**
 * Baixa o PDF da Pré-Nota `preNotaId` (Story 20.5, spec-20-5) — GET
 * /api/pre-notas/{id}/pdf, só `gestor`+ (o servidor revalida o papel:
 * 403 abaixo disso; 404 para Pré-Nota de outra Empresa/inexistente). Molde
 * EXATO de `buscarReciboPedidoBlob`: devolve o `Blob` puro, SEM lógica de
 * DOM — quem cria/clica/remove o `<a download>` é o componente chamador.
 */
export async function baixarPreNotaPDF(preNotaId: string): Promise<Blob> {
  // Falha de rede (`fetch` rejeitando) ou do corpo (`res.blob()`) nunca
  // vaza a mensagem crua do navegador ("Failed to fetch") — sempre a
  // amigável; só a `error.message` do servidor é repassada como veio.
  let res: Response;
  try {
    res = await fetch(apiUrl(`/api/pre-notas/${encodeURIComponent(preNotaId)}/pdf`), {
      headers: authHeaders(),
    });
  } catch {
    throw new Error(MENSAGEM_ERRO_PRE_NOTA);
  }
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_PRE_NOTA);
  }
  try {
    return await res.blob();
  } catch {
    throw new Error(MENSAGEM_ERRO_PRE_NOTA);
  }
}

const MENSAGEM_ERRO_INDICADORES =
  'Não foi possível carregar os indicadores de pedidos agora. Tente novamente em instantes.';

/**
 * Busca os indicadores da faixa de Pedidos (Story 17.4, spec-17-4).
 *
 * `escopo === 'todos'` → GET /api/pedidos/indicadores?escopo=todos (Fila,
 * almoxarife+); omitido → GET /api/pedidos/indicadores (Meus Pedidos).
 * Em ambos os casos o servidor decide o escopo real por papel — este módulo
 * nunca checa papel. Erro lança (chamador usa try/catch e mantém a faixa
 * com "—").
 */
export async function buscarIndicadoresPedidos(
  escopo?: 'todos',
): Promise<{ pendentes: number; aprovadosMes: number; rejeitadosMes: number }> {
  const url =
    escopo === 'todos'
      ? '/api/pedidos/indicadores?escopo=todos'
      : '/api/pedidos/indicadores';
  const res = await fetch(apiUrl(url), { headers: authHeaders() });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
    throw new Error(body.error?.message ?? MENSAGEM_ERRO_INDICADORES);
  }
  return (await res.json()) as { pendentes: number; aprovadosMes: number; rejeitadosMes: number };
}
