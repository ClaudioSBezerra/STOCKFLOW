import { useCallback, useEffect, useRef, useState, type ChangeEvent, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { apiUrl, authHeaders } from '@/lib/api';
import { submeterPedidoBaixa } from '@/lib/pedidos';
import { formatarQuantidade } from '@/components/catalogo/formatacao';

/**
 * Página `/pedidos/baixa` — "Pedido de Baixa de Estoque" (Story 20.2, Epic
 * 20, spec-20-2): solicita a baixa de saldo num Estoque SEM mexer em saldo
 * físico — cria um Pedido `pendente` com a MESMA reserva dura do Pedido de
 * retirada/Transferência, aguardando um Gestor decidir (Story 20.3, fora de
 * escopo aqui). Tela própria, nunca passa pelo Carrinho — o envio é direto
 * de 1 item (Produto + Estoque + quantidade), sem Estoque destino (molde de
 * PedidoTransferenciaPage.tsx, sem o segundo Select).
 *
 * Visível para `usuario`+ (mesmo mínimo de papel do Pedido de retirada/
 * Transferência — `nav-items.ts`). Molde de `LancamentoSaldoSection` (busca
 * de Produto por `GET /api/produtos/busca?q=`) + `CarrinhoPage` (campo
 * Observação do envio de Pedido).
 *
 * SEM campo "Solicitante" (requisito do usuário, pós-revisão: "Solicitante é
 * o usuário logado, não precisa pedir") — o servidor resolve o nome da
 * sessão autenticada, este formulário nunca o pede nem o envia.
 *
 * "Obra / centro de custo" é OPCIONAL (requisito do usuário, pós-revisão,
 * diferente do Pedido de Transferência) — nunca bloqueia o envio vazio.
 *
 * Disponível por Estoque (requisito do usuário, pós-revisão): assim que
 * Produto + Estoque estão escolhidos, busca `GET /api/produtos/{id}` (o
 * MESMO detalhe que `ProdutoDetalhePage` já usa) e mostra
 * `porEstoque[].disponivel` do Estoque selecionado — sem endpoint novo. Um
 * Estoque sem nenhuma linha de saldo para este Produto (nunca movimentado
 * ali) não aparece em `porEstoque`; disponível vira 0.
 *
 * A lista de Estoques (`GET /api/estoques`) é buscada UMA vez, ao montar —
 * cada opção mostra o nome do Estoque junto do nome da Filial
 * (`estoque.filial_nome`). `montadoRef` evita `setState` após desmontar caso
 * uma resposta chegue depois do unmount — mesmo padrão nos dois efeitos de
 * busca abaixo.
 *
 * Quantidade usa `<Input type="number">` NATIVO — nunca parsing manual de
 * string com vírgula/separador de milhar (mesma decisão de spec-20-1).
 */

interface EstoqueOpcao {
  id: string;
  nome: string;
  filial_nome: string | null;
}

interface ProdutoBusca {
  id: string;
  nome: string;
  codigo: string | null;
  categoria: { id: string; codigo: string; nome: string };
  unidadeMedida: string | null;
  embalagem: string | null;
}

// EstoqueDisponivel é o subconjunto de `porEstoque` (GET /api/produtos/{id})
// que esta página precisa — só o suficiente pra mostrar o disponível do
// Estoque escolhido, sem reimportar o tipo inteiro de ProdutoDetalhePage.
interface EstoqueDisponivel {
  estoqueId: string;
  disponivel: number;
}

const DEBOUNCE_MS = 300;

const MENSAGEM_ERRO_BUSCA = 'Não foi possível buscar agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_ESTOQUES =
  'Não foi possível carregar a lista de estoques. Recarregue a página.';
const MENSAGEM_ERRO_SALDO =
  'Não foi possível carregar o saldo disponível agora.';
const MENSAGEM_ERRO_ENVIO =
  'Não foi possível enviar o pedido de baixa agora. Tente novamente em instantes.';

/** Rótulo "Estoque — Filial" (ou só o nome do Estoque, sem Filial cadastrada). */
function rotuloEstoque(estoque: EstoqueOpcao): string {
  return estoque.filial_nome ? `${estoque.nome} — ${estoque.filial_nome}` : estoque.nome;
}

export function PedidoBaixaPage() {
  const [estoques, setEstoques] = useState<EstoqueOpcao[] | null>(null);
  const [erroEstoques, setErroEstoques] = useState<string | null>(null);

  const [termo, setTermo] = useState('');
  const [resultados, setResultados] = useState<ProdutoBusca[] | null>(null);
  const [erroBusca, setErroBusca] = useState(false);
  const [produto, setProduto] = useState<ProdutoBusca | null>(null);

  // porEstoqueDoProduto: disponível por Estoque do Produto escolhido —
  // `null` enquanto carrega (ou sem Produto escolhido), `[]`/lista depois.
  const [porEstoqueDoProduto, setPorEstoqueDoProduto] = useState<EstoqueDisponivel[] | null>(null);
  const [erroSaldo, setErroSaldo] = useState<string | null>(null);

  const [estoqueId, setEstoqueId] = useState('');
  const [quantidade, setQuantidade] = useState('');

  const [obraCentroCusto, setObraCentroCusto] = useState('');
  const [observacao, setObservacao] = useState('');

  const [enviando, setEnviando] = useState(false);
  const [erroEnvio, setErroEnvio] = useState<string | null>(null);

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const termoAtualRef = useRef('');
  const montadoRef = useRef(true);

  useEffect(() => {
    montadoRef.current = true;
    return () => {
      montadoRef.current = false;
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  const carregarEstoques = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/estoques'), { headers: authHeaders() });
      if (!montadoRef.current) return;
      if (!res.ok) {
        setErroEstoques(MENSAGEM_ERRO_ESTOQUES);
        return;
      }
      const body = (await res.json()) as { estoques: EstoqueOpcao[] };
      if (!montadoRef.current) return;
      setEstoques(body.estoques ?? []);
      setErroEstoques(null);
    } catch {
      if (montadoRef.current) {
        setErroEstoques(MENSAGEM_ERRO_ESTOQUES);
      }
    }
  }, []);

  useEffect(() => {
    void carregarEstoques();
  }, [carregarEstoques]);

  // Carrega o disponível por Estoque do Produto escolhido (GET
  // /api/produtos/{id} — o mesmo detalhe que ProdutoDetalhePage já usa,
  // reaproveitado em vez de um endpoint novo).
  useEffect(() => {
    if (!produto) {
      setPorEstoqueDoProduto(null);
      setErroSaldo(null);
      return;
    }
    let cancelado = false;
    setPorEstoqueDoProduto(null);
    setErroSaldo(null);
    fetch(apiUrl(`/api/produtos/${encodeURIComponent(produto.id)}`), { headers: authHeaders() })
      .then(async (res) => {
        if (cancelado || !montadoRef.current) return;
        if (!res.ok) {
          setErroSaldo(MENSAGEM_ERRO_SALDO);
          return;
        }
        const body = (await res.json()) as {
          porEstoque?: { estoqueId: string; disponivel: number }[];
        };
        if (cancelado || !montadoRef.current) return;
        setPorEstoqueDoProduto(
          (body.porEstoque ?? []).map((e) => ({ estoqueId: e.estoqueId, disponivel: e.disponivel })),
        );
      })
      .catch(() => {
        if (!cancelado && montadoRef.current) {
          setErroSaldo(MENSAGEM_ERRO_SALDO);
        }
      });
    return () => {
      cancelado = true;
    };
  }, [produto]);

  function aoDigitarBusca(evento: ChangeEvent<HTMLInputElement>) {
    const valor = evento.target.value;
    setTermo(valor);
    const termoTrimado = valor.trim();
    termoAtualRef.current = termoTrimado;
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
    if (termoTrimado === '') {
      setResultados(null);
      setErroBusca(false);
      return;
    }
    debounceRef.current = setTimeout(() => {
      setErroBusca(false);
      fetch(apiUrl(`/api/produtos/busca?q=${encodeURIComponent(termoTrimado)}`), {
        headers: authHeaders(),
      })
        .then(async (res) => {
          if (!montadoRef.current || termoAtualRef.current !== termoTrimado) return;
          if (!res.ok) {
            setErroBusca(true);
            setResultados(null);
            return;
          }
          const data = (await res.json()) as { produtos?: ProdutoBusca[] };
          if (!montadoRef.current || termoAtualRef.current !== termoTrimado) return;
          setResultados(Array.isArray(data.produtos) ? data.produtos : []);
        })
        .catch(() => {
          if (!montadoRef.current || termoAtualRef.current !== termoTrimado) return;
          setErroBusca(true);
          setResultados(null);
        });
    }, DEBOUNCE_MS);
  }

  function escolherProduto(p: ProdutoBusca) {
    setProduto(p);
    setResultados(null);
    setTermo('');
    termoAtualRef.current = '';
    setErroEnvio(null);
  }

  const quantidadeNumero = Number(quantidade);
  const quantidadeValida =
    quantidade.trim() !== '' && Number.isFinite(quantidadeNumero) && quantidadeNumero > 0;
  const podeEnviar =
    !enviando && produto !== null && estoqueId !== '' && quantidadeValida;

  const disponivelSelecionado =
    produto && estoqueId
      ? (porEstoqueDoProduto?.find((e) => e.estoqueId === estoqueId)?.disponivel ?? 0)
      : null;

  async function enviar(evento: FormEvent<HTMLFormElement>) {
    evento.preventDefault();
    if (!podeEnviar || !produto) {
      return;
    }
    setEnviando(true);
    setErroEnvio(null);
    try {
      await submeterPedidoBaixa({
        obraCentroCusto: obraCentroCusto.trim(),
        observacao: observacao.trim(),
        produtoId: produto.id,
        estoqueId,
        quantidade: quantidadeNumero,
      });
      // Revisão pós-Story 20.2: protege contra setState após desmontar
      // durante o await acima — mesmo padrão `montadoRef.current` já usado
      // nos efeitos de busca de Estoques/Produto neste mesmo arquivo.
      if (!montadoRef.current) return;
      toast.success(
        'Pedido de Baixa enviado. O saldo ficou reservado até a decisão do gestor.',
      );
      setProduto(null);
      setEstoqueId('');
      setQuantidade('');
      setObraCentroCusto('');
      setObservacao('');
    } catch (err) {
      if (!montadoRef.current) return;
      setErroEnvio(err instanceof Error ? err.message : MENSAGEM_ERRO_ENVIO);
    } finally {
      if (montadoRef.current) setEnviando(false);
    }
  }

  return (
    <div className="flex flex-col gap-4 p-6">
      <Card>
        <CardHeader>
          <h1 className="text-heading-lg">Pedido de Baixa de Estoque</h1>
          <CardDescription>
            Solicita a baixa de saldo de um Estoque, sem mexer em saldo físico — o saldo fica
            reservado até um Gestor aprovar.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <form onSubmit={enviar} className="flex flex-col gap-4" noValidate>
            <div className="flex flex-col gap-2">
              <Label htmlFor="baixa-busca-produto">Produto</Label>
              {produto ? (
                <div className="flex flex-col gap-1 rounded-md border border-border p-3">
                  <div className="text-body flex items-center justify-between gap-2">
                    <span className="min-w-0 break-words">
                      {produto.nome}
                      {produto.codigo && (
                        <span className="font-mono text-muted-foreground"> · {produto.codigo}</span>
                      )}
                    </span>
                    <Button type="button" variant="outline" size="sm" onClick={() => setProduto(null)}>
                      Trocar produto
                    </Button>
                  </div>
                </div>
              ) : (
                <>
                  <Input
                    id="baixa-busca-produto"
                    type="search"
                    value={termo}
                    onChange={aoDigitarBusca}
                    placeholder="Buscar por nome, código ou categoria..."
                  />
                  {erroBusca && (
                    <p role="alert" className="text-body text-destructive">
                      {MENSAGEM_ERRO_BUSCA}
                    </p>
                  )}
                  {resultados !== null && resultados.length === 0 && !erroBusca && (
                    <p className="text-body text-muted-foreground">Nenhum produto encontrado.</p>
                  )}
                  {resultados !== null && resultados.length > 0 && (
                    <ul className="flex flex-col gap-2">
                      {resultados.map((p) => (
                        <li key={p.id}>
                          <button
                            type="button"
                            onClick={() => escolherProduto(p)}
                            className="min-h-touch-target-min flex w-full flex-col justify-center gap-1 rounded-md border border-border p-3 text-left"
                          >
                            <span className="text-body">{p.nome}</span>
                            <span className="text-label text-muted-foreground">
                              {p.codigo && <span className="font-mono">{p.codigo}</span>}
                              {p.codigo && ' — '}
                              {p.categoria.nome}
                            </span>
                          </button>
                        </li>
                      ))}
                    </ul>
                  )}
                </>
              )}
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="baixa-estoque">Estoque</Label>
              <Select value={estoqueId} onValueChange={setEstoqueId} disabled={estoques === null}>
                <SelectTrigger id="baixa-estoque" aria-label="Estoque">
                  <SelectValue placeholder={estoques === null ? 'Carregando estoques...' : 'Selecione o estoque'} />
                </SelectTrigger>
                <SelectContent>
                  {(estoques ?? []).map((estoque) => (
                    <SelectItem key={estoque.id} value={estoque.id}>
                      {rotuloEstoque(estoque)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {erroEstoques && (
                <p role="alert" className="text-body text-destructive">
                  {erroEstoques}
                </p>
              )}
              {produto && estoqueId && !erroSaldo && (
                <p className="text-body tabular-nums text-muted-foreground">
                  {porEstoqueDoProduto === null
                    ? 'Carregando saldo disponível...'
                    : `Disponível: ${formatarQuantidade(disponivelSelecionado ?? 0)}`}
                </p>
              )}
              {produto && estoqueId && erroSaldo && (
                <p role="alert" className="text-body text-destructive">
                  {erroSaldo}
                </p>
              )}
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="baixa-quantidade">
                Quantidade{produto?.unidadeMedida ? ` (em ${produto.unidadeMedida})` : ''}
              </Label>
              <Input
                id="baixa-quantidade"
                type="number"
                min="0"
                step="any"
                value={quantidade}
                onChange={(event) => setQuantidade(event.target.value)}
              />
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="baixa-obra">Obra / centro de custo (opcional)</Label>
              <Input
                id="baixa-obra"
                value={obraCentroCusto}
                onChange={(event) => setObraCentroCusto(event.target.value)}
              />
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="baixa-observacao">Observação (opcional)</Label>
              <textarea
                id="baixa-observacao"
                className="min-h-16 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-2 text-base shadow-xs outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 md:text-sm dark:bg-input/30"
                value={observacao}
                onChange={(event) => setObservacao(event.target.value)}
              />
            </div>

            {erroEnvio && (
              <p role="alert" className="text-body text-destructive">
                {erroEnvio}
              </p>
            )}

            <div>
              <Button type="submit" disabled={!podeEnviar}>
                {enviando ? 'Enviando...' : 'Enviar pedido de baixa'}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

export default PedidoBaixaPage;
