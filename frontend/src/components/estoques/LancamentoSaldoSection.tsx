import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type FormEvent,
} from "react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { apiUrl, authHeaders } from "@/lib/api";

/**
 * Aba "Lançar saldo" da página `/estoques` (Story 11.1, FR-47). O Almoxarife
 * escolhe um Produto (busca `GET /api/produtos/busca?q=`, molde de
 * `BuscaCatalogo`), um Estoque (`GET /api/estoques`), informa a quantidade e a
 * validade OPCIONAL e envia `POST /api/lotes`. Cada envio cria SEMPRE um Lote
 * novo no servidor — nunca sobrescreve outro.
 *
 * Sem `window.confirm`; erros inline em `<p role="alert">` (400/404 mostram a
 * mensagem do próprio servidor). O gate de papel (`almoxarife`+) é aplicado
 * por `EstoquesPage` (espelho do servidor, que é a autoridade real).
 */

interface Estoque {
  id: string;
  nome: string;
}

interface ProdutoBusca {
  id: string;
  nome: string;
  codigo: string | null;
  categoria: { id: string; codigo: string; nome: string };
}

const DEBOUNCE_MS = 300;

const MENSAGEM_ERRO_CARREGAR =
  "Não foi possível carregar a lista de estoques. Recarregue a página.";
const MENSAGEM_ERRO_BUSCA =
  "Não foi possível buscar agora. Tente novamente em instantes.";
const MENSAGEM_ERRO_LANCAR =
  "Não foi possível lançar o saldo agora. Tente novamente em instantes.";

// paraNumero aceita vírgula ou ponto decimal; só > 0 habilita o envio.
function paraNumero(valor: string): number {
  return Number(valor.trim().replace(",", "."));
}

export function LancamentoSaldoSection() {
  const [estoques, setEstoques] = useState<Estoque[]>([]);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);

  const [termo, setTermo] = useState("");
  const [resultados, setResultados] = useState<ProdutoBusca[] | null>(null);
  const [erroBusca, setErroBusca] = useState(false);
  const [produto, setProduto] = useState<ProdutoBusca | null>(null);

  const [estoqueId, setEstoqueId] = useState("");
  const [quantidade, setQuantidade] = useState("");
  const [dataValidade, setDataValidade] = useState("");
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const termoAtualRef = useRef("");
  const montadoRef = useRef(true);

  const carregarEstoques = useCallback(async () => {
    try {
      const res = await fetch(apiUrl("/api/estoques"), {
        headers: authHeaders(),
      });
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { estoques: Estoque[] };
      setEstoques(body.estoques ?? []);
      setErroCarregar(null);
    } catch {
      setErroCarregar(MENSAGEM_ERRO_CARREGAR);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await carregarEstoques();
    })();
  }, [carregarEstoques]);

  useEffect(() => {
    montadoRef.current = true;
    return () => {
      montadoRef.current = false;
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, []);

  function aoDigitarBusca(evento: ChangeEvent<HTMLInputElement>) {
    const valor = evento.target.value;
    setTermo(valor);
    const termoTrimado = valor.trim();
    termoAtualRef.current = termoTrimado;
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
    if (termoTrimado === "") {
      setResultados(null);
      setErroBusca(false);
      return;
    }
    debounceRef.current = setTimeout(() => {
      setErroBusca(false);
      fetch(
        apiUrl(`/api/produtos/busca?q=${encodeURIComponent(termoTrimado)}`),
        {
          headers: authHeaders(),
        },
      )
        .then(async (res) => {
          if (!montadoRef.current || termoAtualRef.current !== termoTrimado)
            return;
          if (!res.ok) {
            setErroBusca(true);
            setResultados(null);
            return;
          }
          const data = (await res.json()) as { produtos?: ProdutoBusca[] };
          if (!montadoRef.current || termoAtualRef.current !== termoTrimado)
            return;
          setResultados(Array.isArray(data.produtos) ? data.produtos : []);
        })
        .catch(() => {
          if (!montadoRef.current || termoAtualRef.current !== termoTrimado)
            return;
          setErroBusca(true);
          setResultados(null);
        });
    }, DEBOUNCE_MS);
  }

  function escolherProduto(p: ProdutoBusca) {
    setProduto(p);
    setResultados(null);
    setTermo("");
    termoAtualRef.current = "";
    setErro(null);
  }

  const quantidadeNumero = paraNumero(quantidade);
  const quantidadeValida =
    quantidade.trim() !== "" &&
    Number.isFinite(quantidadeNumero) &&
    quantidadeNumero > 0;
  const podeEnviar =
    produto !== null && estoqueId !== "" && quantidadeValida && !enviando;

  async function enviar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando || !produto || estoqueId === "" || !quantidadeValida) {
      return;
    }
    setErro(null);
    setEnviando(true);
    try {
      const res = await fetch(apiUrl("/api/lotes"), {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders() },
        body: JSON.stringify({
          produtoId: produto.id,
          estoqueId,
          quantidade: quantidadeNumero,
          dataValidade: dataValidade === "" ? null : dataValidade,
        }),
      });
      if (res.ok) {
        toast.success("Saldo lançado.");
        setQuantidade("");
        setDataValidade("");
        return;
      }
      if (res.status === 400 || res.status === 404) {
        const body = (await res.json().catch(() => ({}))) as {
          error?: { message?: string };
        };
        setErro(body.error?.message ?? MENSAGEM_ERRO_LANCAR);
        return;
      }
      setErro(MENSAGEM_ERRO_LANCAR);
    } catch {
      setErro(MENSAGEM_ERRO_LANCAR);
    } finally {
      setEnviando(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <h1 className="text-heading-lg">Lançar saldo</h1>
        <CardDescription>
          Dê entrada de saldo de um Produto num Estoque, com a validade do lote
          quando conhecida. Cada lançamento cria um lote novo.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form onSubmit={enviar} className="flex flex-col gap-4" noValidate>
          <div className="flex flex-col gap-2">
            <Label htmlFor="lote-busca-produto">Produto</Label>
            {produto ? (
              <div className="text-body flex items-center justify-between gap-2 rounded-md border border-border p-3">
                <span className="min-w-0 break-words">
                  {produto.nome}
                  {produto.codigo && (
                    <span className="font-mono text-muted-foreground">
                      {" "}
                      · {produto.codigo}
                    </span>
                  )}
                </span>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => setProduto(null)}
                >
                  Trocar produto
                </Button>
              </div>
            ) : (
              <>
                <Input
                  id="lote-busca-produto"
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
                {resultados !== null &&
                  resultados.length === 0 &&
                  !erroBusca && (
                    <p className="text-body text-muted-foreground">
                      Nenhum produto encontrado.
                    </p>
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
                            {p.codigo && (
                              <span className="font-mono">{p.codigo}</span>
                            )}
                            {p.codigo && " — "}
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
            <Label htmlFor="lote-estoque">Estoque</Label>
            <select
              id="lote-estoque"
              value={estoqueId}
              onChange={(event) => setEstoqueId(event.target.value)}
              className="text-body min-h-touch-target-min rounded-md border border-border bg-background px-3"
            >
              <option value="">Selecione um estoque</option>
              {estoques.map((e) => (
                <option key={e.id} value={e.id}>
                  {e.nome}
                </option>
              ))}
            </select>
            {erroCarregar && (
              <p role="alert" className="text-body text-destructive">
                {erroCarregar}
              </p>
            )}
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="lote-quantidade">Quantidade</Label>
            <Input
              id="lote-quantidade"
              inputMode="decimal"
              value={quantidade}
              onChange={(event) => setQuantidade(event.target.value)}
            />
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="lote-validade">Data de validade (opcional)</Label>
            <Input
              id="lote-validade"
              type="date"
              value={dataValidade}
              onChange={(event) => setDataValidade(event.target.value)}
            />
          </div>

          {erro && (
            <p role="alert" className="text-body text-destructive">
              {erro}
            </p>
          )}

          <div>
            <Button type="submit" disabled={!podeEnviar}>
              {enviando ? "Lançando..." : "Lançar saldo"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

export default LancamentoSaldoSection;
