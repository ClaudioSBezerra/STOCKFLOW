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
import { submeterPedidoTransferencia } from '@/lib/pedidos';
import { formatarQuantidade } from '@/components/catalogo/formatacao';

/**
 * Página `/pedidos/transferencia` — "Pedido de Transferência entre
 * Filiais" (Story 20.1, Epic 20, spec-20-1): solicita uma Transferência de
 * saldo entre Estoques (potencialmente de Filiais diferentes) SEM mexer em
 * saldo físico — cria um Pedido `pendente` com a MESMA reserva dura do
 * Pedido de retirada, aguardando um Gestor decidir (Story 20.3, fora de
 * escopo aqui). Tela própria, nunca passa pelo Carrinho — o envio é direto
 * de 1 item (Produto + Estoque origem + Estoque destino + quantidade).
 *
 * Visível para `usuario`+ (mesmo mínimo de papel do Pedido de retirada —
 * `nav-items.ts`). Molde de `LancamentoSaldoSection` (busca de Produto por
 * `GET /api/produtos/busca?q=`) + `PedidoBaixaPage.tsx` (molde EXATO dos 3
 * ajustes pós-revisão abaixo — mesma tela irmã, só com um segundo Select de
 * destino).
 *
 * SEM campo "Solicitante" (requisito do usuário, pós-revisão, mesmo molde de
 * PedidoBaixaPage.tsx: "Solicitante é o usuário logado, não precisa pedir")
 * — o servidor resolve o nome da sessão autenticada, este formulário nunca o
 * pede nem o envia.
 *
 * "Obra / centro de custo" é OPCIONAL (requisito do usuário, pós-revisão,
 * mesmo molde de PedidoBaixaPage.tsx) — nunca bloqueia o envio vazio.
 *
 * Disponível por Estoque ORIGEM (requisito do usuário, pós-revisão, mesmo
 * molde de PedidoBaixaPage.tsx): assim que Produto + Estoque origem estão
 * escolhidos, busca `GET /api/produtos/{id}` (o MESMO detalhe que
 * `ProdutoDetalhePage` já usa) e mostra `porEstoque[].disponivel` do Estoque
 * ORIGEM selecionado — sem endpoint novo. A Transferência não tem
 * "disponível" de destino (o saldo lá não limita o envio — só a origem é
 * travada/reservada). Um Estoque sem nenhuma linha de saldo para este
 * Produto (nunca movimentado ali) não aparece em `porEstoque`; disponível
 * vira 0.
 *
 * A lista de Estoques (`GET /api/estoques`) é buscada UMA vez, ao montar —
 * cada opção mostra o nome do Estoque junto do nome da Filial
 * (`estoque.filial_nome`), já que a mesma Filial pode ter vários Estoques e
 * duas Filiais diferentes podem ter Estoques de nome parecido. O Estoque
 * destino nunca lista a opção já escolhida como origem (o servidor também
 * rejeita origem==destino, 400); se o usuário trocar a origem DEPOIS de já
 * ter escolhido um destino que agora empata com a nova origem, o destino é
 * resetado (nunca fica selecionado um valor que deixou de ser uma opção
 * válida). `montadoRef` evita `setState` após desmontar caso uma resposta
 * chegue depois do unmount — mesmo padrão nos efeitos de busca abaixo.
 *
 * Quantidade usa `<Input type="number">` NATIVO — nunca parsing manual de
 * string com vírgula/separador de milhar (decisão da revisão da story,
 * Spec Change Log de spec-20-1).
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
// Estoque ORIGEM escolhido, sem reimportar o tipo inteiro de
// ProdutoDetalhePage. Molde de EstoqueDisponivel, PedidoBaixaPage.tsx.
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
  'Não foi possível enviar o pedido de transferência agora. Tente novamente em instantes.';

/** Rótulo "Estoque — Filial" (ou só o nome do Estoque, sem Filial cadastrada). */
function rotuloEstoque(estoque: EstoqueOpcao): string {
  return estoque.filial_nome ? `${estoque.nome} — ${estoque.filial_nome}` : estoque.nome;
}

export function PedidoTransferenciaPage() {
  const [estoques, setEstoques] = useState<EstoqueOpcao[] | null>(null);
  const [erroEstoques, setErroEstoques] = useState<string | null>(null);

  const [termo, setTermo] = useState('');
  const [resultados, setResultados] = useState<ProdutoBusca[] | null>(null);
  const [erroBusca, setErroBusca] = useState(false);
  const [produto, setProduto] = useState<ProdutoBusca | null>(null);

  // porEstoqueDoProduto: disponível por Estoque do Produto escolhido —
  // `null` enquanto carrega (ou sem Produto escolhido), `[]`/lista depois.
  // Molde de PedidoBaixaPage.tsx — só a ORIGEM lê este estado (a
  // Transferência não tem "disponível" de destino).
  const [porEstoqueDoProduto, setPorEstoqueDoProduto] = useState<EstoqueDisponivel[] | null>(null);
  const [erroSaldo, setErroSaldo] = useState<string | null>(null);

  const [estoqueOrigemId, setEstoqueOrigemId] = useState('');
  const [estoqueDestinoId, setEstoqueDestinoId] = useState('');
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
  // reaproveitado em vez de um endpoint novo). Molde EXATO do efeito
  // equivalente em PedidoBaixaPage.tsx.
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
          produto?: { porEstoque?: { estoqueId: string; disponivel: number }[] };
        };
        if (cancelado || !montadoRef.current) return;
        setPorEstoqueDoProduto(
          (body.produto?.porEstoque ?? []).map((e) => ({
            estoqueId: e.estoqueId,
            disponivel: e.disponivel,
          })),
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

  // Trocar a origem reseta o destino se ele empatar com a nova origem — o
  // destino nunca fica selecionado num valor que deixou de ser uma opção
  // válida (Spec Change Log, spec-20-1).
  function aoEscolherOrigem(valor: string) {
    setEstoqueOrigemId(valor);
    if (estoqueDestinoId === valor) {
      setEstoqueDestinoId('');
    }
  }

  const opcoesDestino = (estoques ?? []).filter((e) => e.id !== estoqueOrigemId);

  const quantidadeNumero = Number(quantidade);
  const quantidadeValida =
    quantidade.trim() !== '' && Number.isFinite(quantidadeNumero) && quantidadeNumero > 0;
  const podeEnviar =
    !enviando &&
    produto !== null &&
    estoqueOrigemId !== '' &&
    estoqueDestinoId !== '' &&
    quantidadeValida;

  const disponivelOrigemSelecionado =
    produto && estoqueOrigemId
      ? (porEstoqueDoProduto?.find((e) => e.estoqueId === estoqueOrigemId)?.disponivel ?? 0)
      : null;

  async function enviar(evento: FormEvent<HTMLFormElement>) {
    evento.preventDefault();
    if (!podeEnviar || !produto) {
      return;
    }
    setEnviando(true);
    setErroEnvio(null);
    try {
      await submeterPedidoTransferencia({
        obraCentroCusto: obraCentroCusto.trim(),
        observacao: observacao.trim(),
        produtoId: produto.id,
        estoqueOrigemId,
        estoqueDestinoId,
        quantidade: quantidadeNumero,
      });
      // Revisão pós-Story 20.1 (mesmo molde de PedidoBaixaPage.tsx): protege
      // contra setState após desmontar durante o await acima.
      if (!montadoRef.current) return;
      toast.success(
        'Pedido de Transferência enviado. O saldo da origem ficou reservado até a decisão do gestor.',
      );
      setProduto(null);
      setEstoqueOrigemId('');
      setEstoqueDestinoId('');
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
          <h1 className="text-heading-lg">Pedido de Transferência entre Filiais</h1>
          <CardDescription>
            Solicita a transferência de saldo entre Estoques, sem mexer em saldo físico — o saldo
            da origem fica reservado até um Gestor aprovar.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <form onSubmit={enviar} className="flex flex-col gap-4" noValidate>
            <div className="flex flex-col gap-2">
              <Label htmlFor="transferencia-busca-produto">Produto</Label>
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
                    id="transferencia-busca-produto"
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
              <Label htmlFor="transferencia-origem">Estoque origem</Label>
              <Select value={estoqueOrigemId} onValueChange={aoEscolherOrigem} disabled={estoques === null}>
                <SelectTrigger id="transferencia-origem" aria-label="Estoque origem">
                  <SelectValue placeholder={estoques === null ? 'Carregando estoques...' : 'Selecione a origem'} />
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
              {produto && estoqueOrigemId && !erroSaldo && (
                <p className="text-body tabular-nums text-muted-foreground">
                  {porEstoqueDoProduto === null
                    ? 'Carregando saldo disponível...'
                    : `Disponível: ${formatarQuantidade(disponivelOrigemSelecionado ?? 0)}`}
                </p>
              )}
              {produto && estoqueOrigemId && erroSaldo && (
                <p role="alert" className="text-body text-destructive">
                  {erroSaldo}
                </p>
              )}
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="transferencia-destino">Estoque destino</Label>
              <Select
                value={estoqueDestinoId}
                onValueChange={setEstoqueDestinoId}
                disabled={estoques === null}
              >
                <SelectTrigger id="transferencia-destino" aria-label="Estoque destino">
                  <SelectValue placeholder={estoques === null ? 'Carregando estoques...' : 'Selecione o destino'} />
                </SelectTrigger>
                <SelectContent>
                  {opcoesDestino.map((estoque) => (
                    <SelectItem key={estoque.id} value={estoque.id}>
                      {rotuloEstoque(estoque)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {estoques !== null && opcoesDestino.length === 0 && (
                <p className="text-body text-muted-foreground">
                  Nenhum outro estoque disponível para transferência.
                </p>
              )}
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="transferencia-quantidade">
                Quantidade{produto?.unidadeMedida ? ` (em ${produto.unidadeMedida})` : ''}
              </Label>
              <Input
                id="transferencia-quantidade"
                type="number"
                min="0"
                step="any"
                value={quantidade}
                onChange={(event) => setQuantidade(event.target.value)}
              />
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="transferencia-obra">Obra / centro de custo (opcional)</Label>
              <Input
                id="transferencia-obra"
                value={obraCentroCusto}
                onChange={(event) => setObraCentroCusto(event.target.value)}
              />
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="transferencia-observacao">Observação (opcional)</Label>
              <textarea
                id="transferencia-observacao"
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
                {enviando ? 'Enviando...' : 'Enviar pedido de transferência'}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

export default PedidoTransferenciaPage;
