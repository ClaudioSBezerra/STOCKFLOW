import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { conectarRealtime, type StatusRealtime } from '@/lib/realtime/client';
import { formatarQuantidade } from '@/components/catalogo/formatacao';
import { StatusPedidoBadge } from '@/components/pedidos/StatusPedidoBadge';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import {
  buscarPedido,
  decidirPedido,
  listarFilaPedidos,
  type PedidoItem,
  type PedidoResumo,
  type StatusPedido,
} from '@/lib/pedidos';

/**
 * Seção "Fila" da página `/pedidos` (Story 7.4, spec-7-4) — molde EXATO de
 * `MeusPedidosSection` (mesma árvore de estado, SSE via `conectarRealtime`,
 * `Select` de filtro, `Dialog` "Ver itens" via `buscarPedido`), trocando só:
 * `listarPedidos` -> `listarFilaPedidos` (`GET /api/pedidos?escopo=todos`,
 * MESMA rota — o servidor decide o escopo real, nunca este componente);
 * título "Fila de Pedidos"; mensagens de vazio próprias; toast do evento SSE
 * "Fila de Pedidos atualizada."; e o `aria-label`/`DialogTitle` do "Ver
 * itens" (mesmo formato de `MeusPedidosSection` — solicitante — obra —
 * data/hora — ainda mais necessário aqui, já que a Fila mostra Pedidos de
 * VÁRIOS solicitantes).
 *
 * Renderizada dentro de `PedidosPage`, só quando `podeVerFila` (gate de
 * papel `almoxarife`+ no cliente, espelho de experiência — o servidor
 * continua a autoridade real via `RankPapel` em `ListarPedidosParaSessao`).
 *
 * `FilaPedidosSection` é uma cópia deliberada, não uma abstração
 * compartilhada com `MeusPedidosSection` (Design Notes de spec-7-4): o
 * projeto já tem várias seções de listagem+SSE independentes sem
 * hook/componente genérico — cada "molde de X" é uma cópia intencional.
 */

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar a fila de pedidos agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_ITENS =
  'Não foi possível carregar os itens deste pedido agora. Tente novamente em instantes.';
const MENSAGEM_VAZIO = 'Nenhum pedido na fila.';
const MENSAGEM_VAZIO_FILTRO = 'Nenhum pedido na fila neste status.';
const MENSAGEM_ERRO_DECISAO =
  'Não foi possível registrar a decisão agora. Tente novamente em instantes.';

/** Toast de resultado (Story 7.5, spec-7-5) a partir do NOVO `status` do
 * Pedido devolvido por `decidirPedido` — nunca a partir do que o botão
 * clicado "pretendia" (um clique em "Aprovar" pode resultar em
 * `parcialmente_aprovado`, nunca escondido atrás de uma mensagem genérica
 * de sucesso). */
function mensagemResultadoDecisao(status: StatusPedido): string {
  switch (status) {
    case 'aprovado':
      return 'Pedido aprovado.';
    case 'parcialmente_aprovado':
      return 'Pedido aprovado parcialmente — parte do estoque não estava disponível.';
    case 'rejeitado':
      return 'Pedido rejeitado.';
    default:
      return 'Decisão registrada.';
  }
}

const TODOS = 'todos';
type FiltroStatus = StatusPedido | typeof TODOS;

const OPCOES_FILTRO: Array<{ valor: FiltroStatus; rotulo: string }> = [
  { valor: TODOS, rotulo: 'Todos' },
  { valor: 'pendente', rotulo: 'Pendente' },
  { valor: 'aprovado', rotulo: 'Aprovado' },
  { valor: 'parcialmente_aprovado', rotulo: 'Parcialmente aprovado' },
  { valor: 'rejeitado', rotulo: 'Rejeitado' },
];

export function FilaPedidosSection() {
  const [pedidos, setPedidos] = useState<PedidoResumo[]>([]);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [carregou, setCarregou] = useState(false);
  const [statusConexao, setStatusConexao] = useState<StatusRealtime | null>(null);
  const [filtro, setFiltro] = useState<FiltroStatus>(TODOS);

  // `carregar` lê o filtro corrente por ref para permanecer estável
  // (`useCallback([])`) — a árvore de tempo real não pode reconectar a cada
  // troca de filtro, e a carga inicial/refetch precisam do MESMO caminho.
  const filtroRef = useRef<FiltroStatus>(TODOS);
  const seqRef = useRef(0);

  const carregar = useCallback(async () => {
    const seq = ++seqRef.current;
    const filtroAtual = filtroRef.current;
    try {
      const lista = await listarFilaPedidos(filtroAtual === TODOS ? undefined : filtroAtual);
      if (seq !== seqRef.current) {
        return;
      }
      setPedidos(lista);
      setErroCarregar(null);
      setCarregou(true);
    } catch (err) {
      if (seq === seqRef.current) {
        setErroCarregar(err instanceof Error ? err.message : MENSAGEM_ERRO_CARREGAR);
      }
    }
  }, []);

  useEffect(() => {
    const desconectar = conectarRealtime(
      (evento) => {
        if (evento.resource === 'pedidos') {
          toast.info('Fila de Pedidos atualizada.');
          void carregar();
        }
      },
      (status) => {
        setStatusConexao(status);
        if (status === 'conectado') {
          void carregar();
        }
      },
    );
    return () => {
      desconectar();
    };
  }, [carregar]);

  function aoMudarFiltro(valor: string) {
    const novo = (OPCOES_FILTRO.some((o) => o.valor === valor) ? valor : TODOS) as FiltroStatus;
    filtroRef.current = novo;
    setFiltro(novo);
    void carregar();
  }

  // --- Dialog de itens em snapshot -----------------------------------------
  const [detalheDe, setDetalheDe] = useState<PedidoResumo | null>(null);
  const [itens, setItens] = useState<PedidoItem[] | null>(null);
  const [erroItens, setErroItens] = useState<string | null>(null);
  const [carregandoItens, setCarregandoItens] = useState(false);
  // Guarda anti-corrida (molde de seqRef, usado acima em `carregar`, e de
  // geracaoRef em lib/carrinho.tsx): cada chamada de `abrirItens` — mesmo
  // reabrindo o MESMO Pedido — incrementa `detalheSeqRef`; uma resposta só é
  // aplicada se ainda for a chamada mais recente. Comparar só por id não
  // bastava: reabrir o mesmo Pedido antes da resposta anterior chegar dava
  // ao id da chamada velha e da nova o mesmo valor, deixando a resposta
  // obsoleta (inclusive um erro obsoleto) sobrescrever o resultado já
  // exibido pela reabertura mais recente.
  const detalheSeqRef = useRef(0);

  async function abrirItens(pedido: PedidoResumo) {
    const seq = ++detalheSeqRef.current;
    setDetalheDe(pedido);
    setItens(null);
    setErroItens(null);
    setCarregandoItens(true);
    try {
      const detalhe = await buscarPedido(pedido.id);
      if (seq !== detalheSeqRef.current) {
        return;
      }
      setItens(detalhe.itens);
    } catch (err) {
      if (seq === detalheSeqRef.current) {
        setErroItens(err instanceof Error ? err.message : MENSAGEM_ERRO_ITENS);
      }
    } finally {
      if (seq === detalheSeqRef.current) {
        setCarregandoItens(false);
      }
    }
  }

  // --- Decisão (aprovar/rejeitar) — Story 7.5, spec-7-5 --------------------
  //
  // `confirmacaoPendente` controla qual dos dois ConfirmDialog PRÓPRIOS
  // (Aprovar/Rejeitar) está aberto — texto genérico, sem números ao vivo
  // (Design Notes de spec-7-5: a revalidação real só acontece no POST,
  // nunca num preview). `decidindoId` é o guard de duplo-submit por id
  // (molde de `ConfiguracoesPage.decidir`).
  const [confirmacaoPendente, setConfirmacaoPendente] = useState<'aprovar' | 'rejeitar' | null>(
    null,
  );
  const [decidindoId, setDecidindoId] = useState<string | null>(null);

  async function decidir(pedidoId: string, aprovar: boolean) {
    if (decidindoId !== null) {
      return;
    }
    setDecidindoId(pedidoId);
    try {
      const pedido = await decidirPedido(pedidoId, aprovar);
      // Sucesso: fecha o Dialog "Ver itens" — a lista já re-renderiza pelo
      // refetch que o próprio evento SSE `pedidos` dispara (a decisão
      // publica nesse canal), nenhum refetch manual extra aqui.
      setDetalheDe(null);
      toast.success(mensagemResultadoDecisao(pedido.status));
    } catch (err) {
      // Falha (ex.: 409 — outro Almoxarife já decidiu este Pedido): molde de
      // CarrinhoPage.enviarPedido — toast.error com a mensagem do servidor,
      // Dialog permanece aberto para o usuário ver o que aconteceu.
      toast.error(err instanceof Error ? err.message : MENSAGEM_ERRO_DECISAO);
    } finally {
      setDecidindoId(null);
    }
  }

  const vazio = carregou && !erroCarregar && pedidos.length === 0;

  return (
    <div className="flex flex-col gap-4 p-6">
      <Card>
        <CardHeader>
          <h1 className="text-heading-lg">Fila de Pedidos</h1>
          <CardDescription>
            Todos os pedidos da organização, do mais recente ao mais antigo.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-col gap-1">
            <Label htmlFor="fila-pedidos-filtro-status" className="text-label text-muted-foreground">
              Status
            </Label>
            <Select value={filtro} onValueChange={aoMudarFiltro}>
              <SelectTrigger id="fila-pedidos-filtro-status" className="min-h-touch-target-min w-48">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {OPCOES_FILTRO.map((opcao) => (
                  <SelectItem key={opcao.valor} value={opcao.valor}>
                    {opcao.rotulo}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {statusConexao === 'reconectando' && (
            <output aria-live="polite" className="text-label text-muted-foreground">
              Reconectando...
            </output>
          )}

          {erroCarregar && (
            <p role="alert" className="text-body text-destructive">
              {erroCarregar}
            </p>
          )}

          {!carregou && !erroCarregar && (
            <output className="text-body text-muted-foreground">Carregando pedidos...</output>
          )}

          {vazio && (
            <p className="text-body text-muted-foreground">
              {filtro === TODOS ? MENSAGEM_VAZIO : MENSAGEM_VAZIO_FILTRO}
            </p>
          )}

          {pedidos.length > 0 && (
            <ul className="flex flex-col gap-2">
              {pedidos.map((pedido) => (
                <li
                  key={pedido.id}
                  className="text-body flex flex-wrap items-center justify-between gap-3 border-b border-border pb-3 last:border-b-0 last:pb-0"
                >
                  <div className="flex min-w-0 flex-col gap-0.5">
                    <span className="min-w-0 truncate font-medium">{pedido.solicitante}</span>
                    <span className="text-label text-muted-foreground min-w-0 truncate">
                      {pedido.obraCentroCusto}
                    </span>
                    <span className="text-label text-muted-foreground">
                      {new Date(pedido.criadoEm).toLocaleString('pt-BR')} ·{' '}
                      {pedido.qtdItens} {pedido.qtdItens === 1 ? 'item' : 'itens'}
                    </span>
                  </div>
                  <span className="flex shrink-0 items-center gap-3">
                    <StatusPedidoBadge status={pedido.status} />
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      aria-label={`Ver itens do pedido de ${pedido.solicitante} — ${pedido.obraCentroCusto} — ${new Date(pedido.criadoEm).toLocaleString('pt-BR')}`}
                      onClick={() => void abrirItens(pedido)}
                    >
                      Ver itens
                    </Button>
                  </span>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>

      <Dialog
        open={detalheDe !== null}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setDetalheDe(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {detalheDe
                ? `Itens do pedido — ${detalheDe.solicitante} — ${detalheDe.obraCentroCusto} — ${new Date(detalheDe.criadoEm).toLocaleString('pt-BR')}`
                : 'Itens do pedido'}
            </DialogTitle>
          </DialogHeader>
          {carregandoItens && (
            <output className="text-body text-muted-foreground">Carregando itens...</output>
          )}
          {erroItens && (
            <p role="alert" className="text-body text-destructive">
              {erroItens}
            </p>
          )}
          {itens !== null && !erroItens && (
            <ul className="flex flex-col gap-2">
              {itens.map((item) => {
                // Item já decidido (quantidadeAprovada não-null — o Pedido
                // como um todo deixou de ser `pendente`): mostra Solicitado
                // sempre; Aprovado/Pendente só quando divergem (nunca
                // escondido quando divergem — EXPERIENCE.md, Jornada 2).
                // `!= null` (frouxo) de propósito: cobre tanto `null`
                // explícito (contrato da API) quanto `undefined` (mocks de
                // teste mais antigos, anteriores a esta story, que ainda não
                // incluem o campo) — nenhum dos dois é "decidido".
                const decidido = item.quantidadeAprovada != null;
                const divergente = decidido && item.quantidadeAprovada !== item.quantidade;
                return (
                  <li
                    key={`${item.produtoId}:${item.estoqueId}`}
                    className="text-body flex items-center justify-between gap-4 border-b border-border pb-2 last:border-b-0 last:pb-0"
                  >
                    <div className="flex min-w-0 flex-col">
                      <span className="min-w-0 truncate">{item.produtoNome}</span>
                      <span className="text-label text-muted-foreground">
                        {item.categoriaNome} · {item.estoqueNome}
                      </span>
                    </div>
                    {!decidido && (
                      <span className="tabular-nums shrink-0">
                        {formatarQuantidade(item.quantidade)}
                      </span>
                    )}
                    {decidido && !divergente && (
                      <span className="tabular-nums shrink-0">
                        Solicitado: {formatarQuantidade(item.quantidade)}
                      </span>
                    )}
                    {decidido && divergente && (
                      <span className="tabular-nums flex shrink-0 flex-col items-end gap-0.5">
                        <span>Solicitado: {formatarQuantidade(item.quantidade)}</span>
                        <span className="text-label text-muted-foreground">
                          Aprovado: {formatarQuantidade(item.quantidadeAprovada as number)}
                        </span>
                        <span className="text-label text-muted-foreground">
                          Pendente:{' '}
                          {formatarQuantidade(item.quantidade - (item.quantidadeAprovada as number))}
                        </span>
                      </span>
                    )}
                  </li>
                );
              })}
              {itens.length === 0 && (
                <li className="text-body text-muted-foreground">Este pedido não tem itens.</li>
              )}
            </ul>
          )}

          {detalheDe?.status === 'pendente' && (
            <div className="flex justify-end gap-2 pt-2">
              <Button
                type="button"
                variant="destructive"
                onClick={() => setConfirmacaoPendente('rejeitar')}
                disabled={decidindoId !== null}
              >
                Rejeitar
              </Button>
              <Button
                type="button"
                onClick={() => setConfirmacaoPendente('aprovar')}
                disabled={decidindoId !== null}
              >
                Aprovar
              </Button>
            </div>
          )}
        </DialogContent>
      </Dialog>

      {/* ConfirmDialog PRÓPRIO por ação (Design Notes de spec-7-5): texto
          genérico, sem números ao vivo — a revalidação real só acontece no
          POST de decidirPedido. */}
      <ConfirmDialog
        open={confirmacaoPendente === 'aprovar'}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setConfirmacaoPendente(null);
          }
        }}
        onConfirm={() => {
          setConfirmacaoPendente(null);
          if (detalheDe) {
            void decidir(detalheDe.id, true);
          }
        }}
        title="Aprovar este pedido?"
        description="O estoque disponível é revalidado no momento da aprovação. Um item sem saldo suficiente fica pendente em vez de bloquear o restante. Esta ação não pode ser desfeita."
        confirmLabel="Aprovar"
      />
      <ConfirmDialog
        open={confirmacaoPendente === 'rejeitar'}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setConfirmacaoPendente(null);
          }
        }}
        onConfirm={() => {
          setConfirmacaoPendente(null);
          if (detalheDe) {
            void decidir(detalheDe.id, false);
          }
        }}
        title="Rejeitar este pedido?"
        description="Nenhum item deste pedido será debitado do estoque. Esta ação não pode ser desfeita."
        confirmLabel="Rejeitar"
      />
    </div>
  );
}

export default FilaPedidosSection;
