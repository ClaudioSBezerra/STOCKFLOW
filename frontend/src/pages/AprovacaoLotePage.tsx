import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Checkbox } from '@/components/ui/checkbox';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { TipoPedidoBadge } from '@/components/pedidos/TipoPedidoBadge';
import { BaixarPreNotaButton } from '@/components/pedidos/BaixarPreNotaButton';
import { formatarQuantidade } from '@/components/catalogo/formatacao';
import { conectarRealtime, type StatusRealtime } from '@/lib/realtime/client';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import {
  buscarPedido,
  decidirLotePedidos,
  listarFilaPedidos,
  type PedidoDetalhe,
  type PedidoResumo,
} from '@/lib/pedidos';

/**
 * Página "Aprovação em lote" (`/pedidos/aprovacao-lote`, Story 20.3, Epic
 * 20, spec-20-3) — tela dedicada onde um Gestor (`gestor`/`adm`) seleciona
 * vários Pedidos pendentes de Transferência e/ou Baixa e aprova/rejeita o
 * lote inteiro numa chamada só (`decidirLotePedidos`, POST
 * /api/pedidos/decisao-lote). Molde de `FilaPedidosSection` (SSE via
 * `conectarRealtime`, `ConfirmDialog` próprio por ação) + o padrão de
 * seleção em lote de `InconsistenciasSection` (checkbox por linha +
 * "selecionar todas", Set<string> de ids).
 *
 * Gate `gestor`+ client-side (espelho de `PedidosFilaPage`, que gateia
 * `almoxarife`+) — o servidor continua a autoridade real:
 * `RequireRole(PapelGestor)` roda a cada requisição em
 * POST /api/pedidos/decisao-lote, nível de acesso MAIOR que o `almoxarife`
 * de `DecidirPedido`/Fila de aprovação.
 *
 * Lista os pendentes dos DOIS tipos (`GET /api/pedidos?escopo=todos&status=
 * pendente`, reaproveitado tal qual de `listarFilaPedidos` — gestor já tem
 * rank >= almoxarife, nenhuma mudança de backend necessária), filtrado
 * client-side por `tipo IN (transferencia, baixa)` — NUNCA Pedido de
 * retirada aqui (esse continua só na Fila de aprovação, Story 7.4/7.5).
 * Transferência/Baixa sempre têm exatamente 1 item (Story 20.1/20.2), então
 * a página busca o detalhe de cada Pedido pendente (`buscarPedido`) para
 * mostrar Produto/Quantidade/Estoque direto na lista, sem precisar abrir
 * cada um antes de decidir — o ponto inteiro do lote é decidir sem abrir um
 * por um.
 */

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar os pendentes agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_DECISAO =
  'Não foi possível registrar a decisão do lote agora. Tente novamente em instantes.';
const MENSAGEM_VAZIO = 'Nenhum Pedido de Transferência ou de Baixa pendente.';
const MENSAGEM_ERRO_DETALHE_LINHA =
  'Não foi possível carregar os detalhes deste pedido agora.';

function mensagemResultadoLote(pedidos: PedidoDetalhe[]): string {
  const contagem: Partial<Record<PedidoDetalhe['status'], number>> = {};
  for (const p of pedidos) {
    contagem[p.status] = (contagem[p.status] ?? 0) + 1;
  }
  const partes: string[] = [];
  if (contagem.aprovado) partes.push(`${contagem.aprovado} aprovado(s)`);
  if (contagem.parcialmente_aprovado) {
    partes.push(`${contagem.parcialmente_aprovado} parcialmente aprovado(s)`);
  }
  if (contagem.rejeitado) partes.push(`${contagem.rejeitado} rejeitado(s)`);
  if (contagem.pendente) partes.push(`${contagem.pendente} ainda pendente(s)`);
  return partes.length > 0 ? `Lote decidido: ${partes.join(', ')}.` : 'Lote decidido.';
}

/**
 * Pré-Nota gerada pelo lote (Story 20.4/20.5): todo Pedido decidido AGORA
 * num lote aprovado aponta para a MESMA Pré-Nota — basta o primeiro com
 * `preNotaId`. Rejeição ou lote 100% "pulado" não gera Pré-Nota -> null.
 */
function preNotaDoResultado(
  pedidos: PedidoDetalhe[],
): { id: string; docnum: number | null } | null {
  const comPreNota = pedidos.find((p) => p.preNotaId);
  if (!comPreNota?.preNotaId) return null;
  return { id: comPreNota.preNotaId, docnum: comPreNota.docnum ?? null };
}

function AprovacaoLoteConteudo() {
  const [pendentes, setPendentes] = useState<PedidoResumo[]>([]);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [carregou, setCarregou] = useState(false);
  const [statusConexao, setStatusConexao] = useState<StatusRealtime | null>(null);

  // detalhes: Produto/Quantidade/Estoque de cada Pedido pendente (1 item
  // cada) — carregado à parte da lista de resumos, nunca bloqueia a
  // exibição da lista em si (a linha mostra "Carregando..." enquanto falta).
  const [detalhes, setDetalhes] = useState<Map<string, PedidoDetalhe>>(new Map());
  // errosDetalhe: ids cujo buscarPedido falhou (revisão de código, achado
  // #6: antes essa falha era engolida por um `.catch(() => null)` silencioso
  // e a linha ficava "Carregando..." para sempre, sem erro visível e sem
  // nunca impedir o Gestor de selecionar/decidir um Pedido cujo conteúdo ele
  // nunca viu). Uma linha com erro aqui NUNCA é selecionável (Checkbox
  // desabilitado) e é removida da seleção se já estivesse selecionada
  // enquanto ainda carregava.
  const [errosDetalhe, setErrosDetalhe] = useState<Set<string>>(new Set());

  const [selecionados, setSelecionados] = useState<Set<string>>(new Set());

  const seqRef = useRef(0);

  const carregar = useCallback(async () => {
    const seq = ++seqRef.current;
    try {
      const lista = await listarFilaPedidos('pendente');
      const doLote = lista.filter((p) => p.tipo === 'transferencia' || p.tipo === 'baixa');
      if (seq !== seqRef.current) return;
      setPendentes(doLote);
      setErroCarregar(null);
      setCarregou(true);

      const idsAtuais = new Set(doLote.map((p) => p.id));
      // Remove da seleção qualquer id que deixou de estar pendente (já
      // decidido por outra requisição, inclusive em outro lote).
      setSelecionados((atual) => {
        const novo = new Set(Array.from(atual).filter((id) => idsAtuais.has(id)));
        return novo.size === atual.size ? atual : novo;
      });
      // Detalhe/erro de um Pedido que saiu da lista de pendentes não fazem
      // mais sentido (evita a Map/Set crescerem sem teto ao longo de várias
      // recargas via SSE).
      setDetalhes((atual) => {
        const novo = new Map(Array.from(atual).filter(([id]) => idsAtuais.has(id)));
        return novo.size === atual.size ? atual : novo;
      });
      setErrosDetalhe((atual) => {
        const novo = new Set(Array.from(atual).filter((id) => idsAtuais.has(id)));
        return novo.size === atual.size ? atual : novo;
      });

      const resultados = await Promise.all(
        doLote.map((p) =>
          buscarPedido(p.id)
            .then((d) => ({ id: p.id, detalhe: d as PedidoDetalhe | null, erro: false }))
            .catch(() => ({ id: p.id, detalhe: null as PedidoDetalhe | null, erro: true })),
        ),
      );
      if (seq !== seqRef.current) return;

      setDetalhes((atual) => {
        const novo = new Map(atual);
        for (const r of resultados) {
          if (r.detalhe) novo.set(r.id, r.detalhe);
        }
        return novo;
      });
      const novosErros = new Set(resultados.filter((r) => r.erro).map((r) => r.id));
      setErrosDetalhe(novosErros);
      if (novosErros.size > 0) {
        // Nunca deixa uma linha com erro continuar selecionada — o Gestor
        // nunca decide um Pedido cujo conteúdo nunca foi exibido.
        setSelecionados((atual) => {
          const novo = new Set(Array.from(atual).filter((id) => !novosErros.has(id)));
          return novo.size === atual.size ? atual : novo;
        });
      }
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

  const alternarSelecao = useCallback(
    (id: string) => {
      // Defesa em profundidade: o Checkbox da linha já vem `disabled` para
      // um id com erro de carga (ver JSX abaixo), mas este guard garante
      // que NENHUM caminho de código consiga selecionar um Pedido cujo
      // conteúdo nunca foi exibido.
      if (errosDetalhe.has(id)) return;
      setSelecionados((atual) => {
        const novo = new Set(atual);
        if (novo.has(id)) {
          novo.delete(id);
        } else {
          novo.add(id);
        }
        return novo;
      });
    },
    [errosDetalhe],
  );

  // selecionaveis: pendentes SEM erro de carga — "Selecionar todos" nunca
  // marca uma linha com erro (Checkbox dela fica desabilitado de qualquer
  // forma, mas a contagem/"todasSelecionadas" não deve considerá-la).
  const selecionaveis = useMemo(
    () => pendentes.filter((p) => !errosDetalhe.has(p.id)),
    [pendentes, errosDetalhe],
  );

  const todasSelecionadas = useMemo(
    () => selecionaveis.length > 0 && selecionaveis.every((p) => selecionados.has(p.id)),
    [selecionaveis, selecionados],
  );

  const alternarSelecaoTodas = useCallback(() => {
    setSelecionados((atual) => {
      const todasJaSelecionadas =
        selecionaveis.length > 0 && selecionaveis.every((p) => atual.has(p.id));
      if (todasJaSelecionadas) return new Set();
      return new Set(selecionaveis.map((p) => p.id));
    });
  }, [selecionaveis]);

  // algumaSelecionadaCarregando: ao menos um Pedido SELECIONADO ainda não
  // terminou de carregar o detalhe (nem sucesso nem erro) — enquanto isso,
  // os botões de decisão ficam desabilitados (Revisão de código, achado #6:
  // nunca decidir um Pedido cujo conteúdo o Gestor ainda não viu, mesmo que
  // ele já tenha marcado o checkbox antes da resposta chegar).
  const algumaSelecionadaCarregando = useMemo(
    () =>
      Array.from(selecionados).some((id) => !detalhes.has(id) && !errosDetalhe.has(id)),
    [selecionados, detalhes, errosDetalhe],
  );

  // --- Decisão em lote (aprovar/rejeitar os selecionados) -------------------
  const [confirmacaoPendente, setConfirmacaoPendente] = useState<'aprovar' | 'rejeitar' | null>(
    null,
  );
  const [decidindo, setDecidindo] = useState(false);
  const [erroDecisao, setErroDecisao] = useState<string | null>(null);
  // preNotaGerada (Story 20.5): Pré-Nota do ÚLTIMO lote decidido com sucesso
  // nesta tela — só é substituída (ou limpa, numa rejeição) quando uma nova
  // decisão dá certo; uma decisão que falha nunca some com o link anterior.
  const [preNotaGerada, setPreNotaGerada] = useState<{ id: string; docnum: number | null } | null>(
    null,
  );

  async function decidirSelecionados(aprovar: boolean) {
    if (decidindo || selecionados.size === 0) {
      return;
    }
    setDecidindo(true);
    setErroDecisao(null);
    try {
      const resultado = await decidirLotePedidos(Array.from(selecionados), aprovar);
      setSelecionados(new Set());
      setPreNotaGerada(preNotaDoResultado(resultado));
      toast.success(mensagemResultadoLote(resultado));
      await carregar();
    } catch (err) {
      setErroDecisao(err instanceof Error ? err.message : MENSAGEM_ERRO_DECISAO);
    } finally {
      setDecidindo(false);
    }
  }

  const vazio = carregou && !erroCarregar && pendentes.length === 0;

  return (
    <Card>
      <CardHeader>
        <h1 className="text-heading-lg">Aprovação em lote</h1>
        <CardDescription>
          Selecione vários Pedidos de Transferência e/ou Baixa pendentes e aprove ou rejeite o lote
          inteiro numa chamada só. O estoque disponível é revalidado item a item no momento da
          aprovação — um item sem saldo suficiente fica parcialmente aprovado, sem travar o resto do
          lote.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
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
          <output className="text-body text-muted-foreground">Carregando pendentes...</output>
        )}

        {preNotaGerada && (
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border p-3">
            <output aria-live="polite" className="text-body">
              {preNotaGerada.docnum !== null
                ? `Pré-Nota nº ${preNotaGerada.docnum} gerada.`
                : 'Pré-Nota gerada.'}
            </output>
            <BaixarPreNotaButton preNotaId={preNotaGerada.id} docnum={preNotaGerada.docnum} />
          </div>
        )}

        {vazio && <p className="text-body text-muted-foreground">{MENSAGEM_VAZIO}</p>}

        {erroDecisao && (
          <p role="alert" className="text-body text-destructive">
            {erroDecisao}
          </p>
        )}

        {pendentes.length > 0 && (
          <>
            <div className="flex gap-2">
              <Button
                type="button"
                variant="destructive"
                disabled={decidindo || selecionados.size === 0 || algumaSelecionadaCarregando}
                onClick={() => setConfirmacaoPendente('rejeitar')}
              >
                Rejeitar selecionados ({selecionados.size})
              </Button>
              <Button
                type="button"
                disabled={decidindo || selecionados.size === 0 || algumaSelecionadaCarregando}
                onClick={() => setConfirmacaoPendente('aprovar')}
              >
                {decidindo ? 'Decidindo...' : `Aprovar selecionados (${selecionados.size})`}
              </Button>
            </div>

            <div className="overflow-x-auto">
              <table className="w-full text-body">
                <thead>
                  <tr className="text-label text-muted-foreground">
                    <th className="py-2 pr-4 text-left font-medium">
                      <Checkbox
                        aria-label="Selecionar todos"
                        checked={todasSelecionadas}
                        onCheckedChange={alternarSelecaoTodas}
                        disabled={decidindo}
                      />
                    </th>
                    <th className="py-2 pr-4 text-left font-medium">Solicitante</th>
                    <th className="py-2 pr-4 text-left font-medium">Tipo</th>
                    <th className="py-2 pr-4 text-left font-medium">Produto</th>
                    <th className="py-2 pr-4 text-left font-medium">Quantidade</th>
                    <th className="py-2 pr-4 text-left font-medium">Estoque</th>
                    <th className="py-2 text-left font-medium">Data</th>
                  </tr>
                </thead>
                <tbody>
                  {pendentes.map((pedido) => {
                    const detalhe = detalhes.get(pedido.id);
                    const item = detalhe?.itens[0];
                    const erroLinha = errosDetalhe.has(pedido.id);
                    return (
                      <tr key={pedido.id} className="border-t border-border">
                        <td className="py-2 pr-4">
                          <Checkbox
                            aria-label={`Selecionar pedido de ${pedido.solicitante} — ${pedido.obraCentroCusto}`}
                            checked={selecionados.has(pedido.id)}
                            onCheckedChange={() => alternarSelecao(pedido.id)}
                            disabled={decidindo || erroLinha}
                          />
                        </td>
                        <td className="py-2 pr-4">{pedido.solicitante}</td>
                        <td className="py-2 pr-4">
                          <TipoPedidoBadge tipo={pedido.tipo} />
                        </td>
                        {erroLinha ? (
                          <td className="py-2 pr-4" colSpan={3}>
                            <span role="alert" className="text-destructive">
                              {MENSAGEM_ERRO_DETALHE_LINHA}
                            </span>{' '}
                            <button
                              type="button"
                              className="underline"
                              onClick={() => void carregar()}
                            >
                              Tentar novamente
                            </button>
                          </td>
                        ) : (
                          <>
                            <td className="py-2 pr-4">
                              {item ? item.produtoNome : 'Carregando...'}
                            </td>
                            <td className="py-2 pr-4 tabular-nums">
                              {item ? formatarQuantidade(item.quantidade) : '—'}
                            </td>
                            <td className="py-2 pr-4">{item ? item.estoqueNome : '—'}</td>
                          </>
                        )}
                        <td className="py-2">{new Date(pedido.criadoEm).toLocaleString('pt-BR')}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </>
        )}
      </CardContent>

      <ConfirmDialog
        open={confirmacaoPendente === 'aprovar'}
        onOpenChange={(aberto) => {
          if (!aberto) setConfirmacaoPendente(null);
        }}
        onConfirm={() => {
          setConfirmacaoPendente(null);
          void decidirSelecionados(true);
        }}
        title={`Aprovar ${selecionados.size} pedido(s)?`}
        description="O estoque disponível é revalidado item a item no momento da aprovação. Um item sem saldo suficiente fica parcialmente aprovado, sem travar o resto do lote. Esta ação não pode ser desfeita."
        confirmLabel="Aprovar"
      />
      <ConfirmDialog
        open={confirmacaoPendente === 'rejeitar'}
        onOpenChange={(aberto) => {
          if (!aberto) setConfirmacaoPendente(null);
        }}
        onConfirm={() => {
          setConfirmacaoPendente(null);
          void decidirSelecionados(false);
        }}
        title={`Rejeitar ${selecionados.size} pedido(s)?`}
        description="Nenhum item dos pedidos selecionados será debitado do estoque. Esta ação não pode ser desfeita."
        confirmLabel="Rejeitar"
        confirmVariant="destructive"
      />
    </Card>
  );
}

/** Gate `gestor`+ client-side, mesmo molde de `PedidosFilaPage`
 * (`almoxarife`+) — o servidor continua a autoridade real
 * (`RequireRole(PapelGestor)` em POST /api/pedidos/decisao-lote). */
export function AprovacaoLotePage() {
  const { usuario } = useAuth();
  const podeAprovarLote = rankPapel(usuario?.papel ?? '') >= rankPapel('gestor');

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeAprovarLote ? (
        <AprovacaoLoteConteudo />
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso à Aprovação em lote.
        </p>
      )}
    </div>
  );
}

export default AprovacaoLotePage;
