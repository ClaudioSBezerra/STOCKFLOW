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
import { useAuth } from '@/lib/auth';
import { apiUrl, authHeaders } from '@/lib/api';
import { submeterPedidoTransferencia } from '@/lib/pedidos';

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
 * `GET /api/produtos/busca?q=`) + `CarrinhoPage` (campos Solicitante/Obra ou
 * centro de custo/Observação do envio de Pedido).
 *
 * A lista de Estoques (`GET /api/estoques`) é buscada UMA vez, ao montar —
 * cada opção mostra o nome do Estoque junto do nome da Filial
 * (`estoque.filial_nome`), já que a mesma Filial pode ter vários Estoques e
 * duas Filiais diferentes podem ter Estoques de nome parecido. O Estoque
 * destino nunca lista a opção já escolhida como origem (o servidor também
 * rejeita origem==destino, 400); se o usuário trocar a origem DEPOIS de já
 * ter escolhido um destino que agora empata com a nova origem, o destino é
 * resetado (nunca fica selecionado um valor que deixou de ser uma opção
 * válida). `montadoRef` evita `setState` após desmontar caso a resposta de
 * `GET /api/estoques` chegue depois do unmount — mesmo padrão do efeito de
 * busca de Produto abaixo.
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

const DEBOUNCE_MS = 300;

const MENSAGEM_ERRO_BUSCA = 'Não foi possível buscar agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_ESTOQUES =
  'Não foi possível carregar a lista de estoques. Recarregue a página.';
const MENSAGEM_ERRO_ENVIO =
  'Não foi possível enviar o pedido de transferência agora. Tente novamente em instantes.';

/** Rótulo "Estoque — Filial" (ou só o nome do Estoque, sem Filial cadastrada). */
function rotuloEstoque(estoque: EstoqueOpcao): string {
  return estoque.filial_nome ? `${estoque.nome} — ${estoque.filial_nome}` : estoque.nome;
}

export function PedidoTransferenciaPage() {
  const { usuario } = useAuth();

  const [estoques, setEstoques] = useState<EstoqueOpcao[] | null>(null);
  const [erroEstoques, setErroEstoques] = useState<string | null>(null);

  const [termo, setTermo] = useState('');
  const [resultados, setResultados] = useState<ProdutoBusca[] | null>(null);
  const [erroBusca, setErroBusca] = useState(false);
  const [produto, setProduto] = useState<ProdutoBusca | null>(null);

  const [estoqueOrigemId, setEstoqueOrigemId] = useState('');
  const [estoqueDestinoId, setEstoqueDestinoId] = useState('');
  const [quantidade, setQuantidade] = useState('');

  const [solicitante, setSolicitante] = useState(usuario?.nome ?? '');
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
    quantidadeValida &&
    solicitante.trim() !== '' &&
    obraCentroCusto.trim() !== '';

  async function enviar(evento: FormEvent<HTMLFormElement>) {
    evento.preventDefault();
    if (!podeEnviar || !produto) {
      return;
    }
    setEnviando(true);
    setErroEnvio(null);
    try {
      await submeterPedidoTransferencia({
        solicitante: solicitante.trim(),
        obraCentroCusto: obraCentroCusto.trim(),
        observacao: observacao.trim(),
        produtoId: produto.id,
        estoqueOrigemId,
        estoqueDestinoId,
        quantidade: quantidadeNumero,
      });
      toast.success(
        'Pedido de Transferência enviado. O saldo da origem ficou reservado até a decisão do gestor.',
      );
      setProduto(null);
      setEstoqueOrigemId('');
      setEstoqueDestinoId('');
      setQuantidade('');
      setObservacao('');
    } catch (err) {
      setErroEnvio(err instanceof Error ? err.message : MENSAGEM_ERRO_ENVIO);
    } finally {
      setEnviando(false);
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
              {erroEstoques && (
                <p role="alert" className="text-body text-destructive">
                  {erroEstoques}
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
              <Label htmlFor="transferencia-solicitante">Solicitante</Label>
              <Input
                id="transferencia-solicitante"
                value={solicitante}
                onChange={(event) => setSolicitante(event.target.value)}
              />
            </div>

            <div className="flex flex-col gap-2">
              <Label htmlFor="transferencia-obra">Obra / centro de custo</Label>
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
