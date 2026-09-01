import { useCallback, useEffect, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { XIcon } from 'lucide-react';
import { toast } from 'sonner';
import { Card, CardContent, CardHeader } from '@/components/ui/card';
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { getAccessToken } from '@/lib/session';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { conectarRealtime, type StatusRealtime } from '@/lib/realtime/client';
import {
  formatarQuantidade,
  IndicadorDisponibilidade,
  resumirDimensoes,
  type Dimensoes,
} from '@/components/catalogo/formatacao';

/**
 * Página de detalhe do Produto (Story 4.4, spec-4-4, rota `/produtos/:id`,
 * filha de `RotaProtegida` — sem gate de papel próprio, `usuario`+):
 * alcançável clicando um card da grade (Story 4.3) ou um resultado de busca
 * (Story 4.1). Mostra nome, código (`font-mono` quando presente), categoria,
 * dimensões (`resumirDimensoes`), indicador de disponibilidade
 * (`IndicadorDisponibilidade`) e a quantidade discriminada por Estoque —
 * mesma formatação de `CatalogoListagem`/`BuscaCatalogo` (`@/components/
 * catalogo/formatacao`).
 *
 * A busca inicial E o refetch pós-reconexão são o MESMO caminho de código:
 * `carregarDetalhe` só é chamado a partir de `aoMudarStatus('conectado')`
 * (dispara também na primeira conexão) — nunca de um `useEffect` de mount
 * separado (AD-3: "sempre GET completo ao reconectar", unificar os dois
 * evita dois caminhos divergentes para a mesma responsabilidade). A tela
 * assina o canal `produtos` via `conectarRealtime`; um evento sobre o
 * MESMO Produto (`resource==='produtos' && id===<id da rota>`) dispara um
 * refetch completo (mesma `carregarDetalhe`) + `toast.info('Catálogo
 * atualizado.')` (`sonner`, `aria-live="polite"` nativo do `Toaster`
 * global). Status `'reconectando'` mostra um `<output>` persistente
 * "Reconectando..." (`aria-live="polite"`) enquanto durar. Unmount
 * desconecta a SSE.
 *
 * O React Router NÃO remonta um componente de rota quando só o `:id` da
 * rota muda (a mesma rota casa `/produtos/A` e `/produtos/B`) — sem
 * cuidado extra, uma resposta em voo do produto ANTIGO poderia chegar
 * depois da troca e sobrescrever a tela com o produto ERRADO. Por isso
 * `ProdutoDetalhePage` é só um wrapper fino: repassa `id` para
 * `ProdutoDetalheConteudo` com `key={id}` — trocar o `key` força o React a
 * desmontar a instância antiga e montar uma instância nova (estado,
 * `seqRef` e `objectUrlCacheRef` sempre partem zerados para cada produto,
 * e qualquer resposta tardia da instância antiga cai num componente já
 * desmontado, sem efeito). Dentro de `ProdutoDetalheConteudo`, um `seqRef`
 * (mesmo padrão de `CatalogoListagem`/`BuscaCatalogo`) ainda descarta
 * respostas obsoletas de chamadas sobrepostas para o MESMO id (ex.: dois
 * refetches disparados em sequência rápida).
 *
 * Fotos (Story 3.6): `GET /api/produtos/{id}/fotos` + fetch-com-auth +
 * `blob()` + `URL.createObjectURL` por foto (mesmo padrão de
 * `CadastroProdutoSection.tsx` — um `<img src>` direto na URL da API
 * falharia, a rota é `RequireAuth` e `<img>` não envia `Authorization`),
 * miniaturas em grade abrindo lightbox em tela cheia (`Dialog`, mesmo molde
 * de `CadastroProdutoSection.tsx`, reaproveitado aqui sem importar
 * diretamente — estado local incompatível). 0 fotos -> sem seção de fotos,
 * sem erro. Se `fotos` mudar (ex.: refetch por evento SSE) enquanto o
 * lightbox está aberto num índice que deixou de existir no novo array, o
 * `Dialog` é fechado — `open` é derivado de `fotos[lightboxIndex]` existir,
 * nunca guardado como um booleano solto que poderia dessincronizar.
 *
 * Registrar Baixa (Story 5.1, spec-5-1): cada linha de "Quantidade por
 * Estoque" ganha um botão "Registrar Baixa" (`variant="outline"`,
 * `size="sm"`), visível só quando `rankPapel(usuario?.papel ?? '') >=
 * rankPapel('almoxarife')` (molde de `podeCadastrar`/`podeExportar`,
 * `CatalogoPage.tsx`) — o servidor continua a autoridade real (403 para
 * `usuario` mesmo que o botão nunca apareça, `RequireRole` decide). Clique
 * abre um `Dialog` (estado `baixaEstoque`, distinto do lightbox de fotos)
 * com um `Input type="number"` para a quantidade. Confirmar dispara
 * `POST /api/produtos/{id}/estoques/{estoqueId}/baixa`; sucesso ->
 * `toast.success`, fecha o diálogo e refaz `carregarDetalhe()` (mesma busca
 * usada no mount/reconexão/refetch por SSE — nenhum caminho de atualização
 * de estado paralelo); falha mostra a mensagem do servidor (que já cita a
 * quantidade disponível no 409) DENTRO do diálogo, sem fechar.
 */

interface CategoriaDetalhe {
  id: string;
  codigo: string;
  nome: string;
}

interface EstoqueQuantidade {
  estoqueId: string;
  estoqueNome: string;
  quantidade: number;
}

interface ProdutoDetalhe {
  id: string;
  nome: string;
  codigo: string | null;
  categoria: CategoriaDetalhe;
  dimensoes: Dimensoes;
  quantidadeTotal: number;
  disponivel: boolean;
  porEstoque: EstoqueQuantidade[];
}

interface FotoGaleria {
  nome: string;
  url: string;
  objectUrl: string;
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

type ErroDetalhe = 'nao-encontrado' | 'generico';

const MENSAGEM_ERRO = 'Não foi possível carregar o produto agora. Tente novamente em instantes.';
const MENSAGEM_NAO_ENCONTRADO = 'Produto não encontrado.';
const MENSAGEM_SEM_ESTOQUE_REGISTRADO = 'Sem quantidade registrada por estoque.';
const MENSAGEM_ERRO_BAIXA = 'Não foi possível registrar a baixa agora. Tente novamente em instantes.';

// ProdutoDetalhePage: wrapper fino de roteamento — ver doc acima sobre por
// que `key={id}` é essencial aqui (força remontagem completa a cada troca
// de produto, em vez de reaproveitar a mesma instância entre ids
// diferentes).
export function ProdutoDetalhePage() {
  const { id } = useParams<{ id: string }>();

  if (!id) {
    return null;
  }

  return <ProdutoDetalheConteudo key={id} id={id} />;
}

function ProdutoDetalheConteudo({ id }: { id: string }) {
  const { usuario } = useAuth();
  const podeRegistrarBaixa = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  const [produto, setProduto] = useState<ProdutoDetalhe | null>(null);
  const [carregando, setCarregando] = useState(true);
  const [erro, setErro] = useState<ErroDetalhe | null>(null);
  const [statusConexao, setStatusConexao] = useState<StatusRealtime | null>(null);

  const [fotos, setFotos] = useState<FotoGaleria[]>([]);
  const [lightboxIndex, setLightboxIndex] = useState<number | null>(null);
  const objectUrlCacheRef = useRef<Map<string, string>>(new Map());

  // Diálogo de Registrar Baixa (Story 5.1): `baixaEstoque` guarda a linha
  // (estoqueId/estoqueNome) alvo — `null` fecha o diálogo, mesmo padrão
  // derivado de `fotoLightbox` abaixo (nenhum booleano solto separado).
  const [baixaEstoque, setBaixaEstoque] = useState<EstoqueQuantidade | null>(null);
  const [quantidadeBaixa, setQuantidadeBaixa] = useState('');
  const [enviandoBaixa, setEnviandoBaixa] = useState(false);
  const [erroBaixa, setErroBaixa] = useState<string | null>(null);

  // seqRef descarta qualquer resposta em voo que não corresponda mais à
  // chamada mais recente (mesma guarda de CatalogoListagem/BuscaCatalogo)
  // — cobre chamadas sobrepostas para o MESMO id (ex.: dois refetches em
  // sequência). A troca de id em si é coberta pelo `key={id}` do wrapper
  // acima, que já descarta a instância inteira.
  const seqRef = useRef(0);

  // Revoga todos os Object URLs em cache quando o componente desmonta —
  // mesmo cuidado de CadastroProdutoSection. Como cada produto tem sua
  // própria instância (key={id}), este cache nunca é compartilhado entre
  // produtos diferentes.
  useEffect(() => {
    const cache = objectUrlCacheRef.current;
    return () => {
      for (const url of cache.values()) {
        URL.revokeObjectURL(url);
      }
    };
  }, []);

  // carregarFotos busca a galeria e resolve o Object URL de cada foto ainda
  // ausente do cache local — uma miniatura já resolvida nunca é rebuscada.
  // Falha aqui é silenciosa (a tela de detalhe é só consulta — Never da
  // spec — e "0 fotos" já é um estado válido sem erro): a galeria
  // simplesmente não aparece/atualiza nessa rodada. `seq` descarta o
  // resultado inteiro (ou parcial, entre fotos) se um refetch mais novo já
  // tiver começado.
  const carregarFotos = useCallback(async (produtoId: string, seq: number) => {
    try {
      const res = await fetch(`/api/produtos/${produtoId}/fotos`, { headers: authHeaders() });
      if (seq !== seqRef.current) return;
      if (!res.ok) return;
      const body = (await res.json()) as { fotos: { nome: string; url: string }[] };
      if (seq !== seqRef.current) return;

      const itens: FotoGaleria[] = [];
      for (const foto of body.fotos) {
        if (seq !== seqRef.current) return;
        let objectUrl = objectUrlCacheRef.current.get(foto.nome);
        if (!objectUrl) {
          const resFoto = await fetch(foto.url, { headers: authHeaders() });
          if (seq !== seqRef.current) return;
          if (!resFoto.ok) return;
          const blob = await resFoto.blob();
          if (seq !== seqRef.current) return;
          objectUrl = URL.createObjectURL(blob);
          objectUrlCacheRef.current.set(foto.nome, objectUrl);
        }
        itens.push({ nome: foto.nome, url: foto.url, objectUrl });
      }
      if (seq !== seqRef.current) return;
      setFotos(itens);
    } catch {
      // silencioso — ver comentário acima.
    }
  }, []);

  // carregarDetalhe é o ÚNICO caminho de busca do Produto — chamado tanto na
  // primeira conexão SSE quanto em qualquer reconexão/refetch por evento
  // (ver doc do componente acima). Incrementa `seqRef` a cada chamada: uma
  // resposta que chega depois de uma chamada mais nova (outro refetch já
  // disparado) é descartada em vez de sobrescrever a tela. 404 mostra
  // MENSAGEM_NAO_ENCONTRADO — distinto do erro genérico (500/rede), que
  // sugere tentar de novo (um produto que não existe nunca vai aparecer).
  const carregarDetalhe = useCallback(async () => {
    const seq = ++seqRef.current;
    setErro(null);
    setCarregando(true);
    try {
      const res = await fetch(`/api/produtos/${id}`, { headers: authHeaders() });
      if (seq !== seqRef.current) return;
      if (!res.ok) {
        setErro(res.status === 404 ? 'nao-encontrado' : 'generico');
        return;
      }
      const data = (await res.json()) as { produto: ProdutoDetalhe };
      if (seq !== seqRef.current) return;
      setProduto(data.produto);
      await carregarFotos(id, seq);
    } catch {
      if (seq === seqRef.current) {
        setErro('generico');
      }
    } finally {
      if (seq === seqRef.current) {
        setCarregando(false);
      }
    }
  }, [id, carregarFotos]);

  // confirmarBaixa envia POST /api/produtos/{id}/estoques/{estoqueId}/baixa
  // para a linha guardada em `baixaEstoque` (molde exato do POST de
  // CadastroProdutoSection.tsx: headers com authHeaders(), body JSON). Defesa
  // em profundidade contra duplo-submit (`desabilitado`, mesmo padrão de
  // CadastroProdutoSection): o `disabled` do botão só reflete `enviandoBaixa`
  // após o próximo repaint. Sucesso -> toast + fecha o diálogo + refetch via
  // carregarDetalhe (MESMA função do mount/reconexão/SSE, nunca um caminho
  // de atualização de estado paralelo); falha mantém o diálogo aberto e
  // mostra a mensagem do servidor (envelope AD-14, já cita a quantidade
  // disponível no 409) — nunca uma string genérica fixa quando o servidor
  // devolveu uma.
  async function confirmarBaixa() {
    if (!baixaEstoque || enviandoBaixa || quantidadeBaixa.trim() === '') {
      return;
    }
    const quantidade = Number(quantidadeBaixa);
    if (!Number.isFinite(quantidade)) {
      setErroBaixa('Quantidade inválida.');
      return;
    }
    setEnviandoBaixa(true);
    setErroBaixa(null);
    try {
      const res = await fetch(`/api/produtos/${id}/estoques/${baixaEstoque.estoqueId}/baixa`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ quantidade }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
        setErroBaixa(body.error?.message ?? MENSAGEM_ERRO_BAIXA);
        return;
      }
      toast.success('Baixa registrada.');
      setBaixaEstoque(null);
      setQuantidadeBaixa('');
      await carregarDetalhe();
    } catch {
      setErroBaixa(MENSAGEM_ERRO_BAIXA);
    } finally {
      setEnviandoBaixa(false);
    }
  }

  useEffect(() => {
    const desconectar = conectarRealtime(
      (evento) => {
        if (evento.resource === 'produtos' && evento.id === id) {
          toast.info('Catálogo atualizado.');
          void carregarDetalhe();
        }
      },
      (status) => {
        setStatusConexao(status);
        if (status === 'conectado') {
          void carregarDetalhe();
        }
      },
    );
    return () => {
      desconectar();
    };
  }, [id, carregarDetalhe]);

  // fotoLightbox é derivada — nunca guardamos um booleano "lightbox aberto"
  // separado do índice: se `fotos` mudar (ex.: refetch por evento SSE) e o
  // índice aberto deixar de existir no novo array, `fotoLightbox` vira
  // `null` no mesmo render e o `Dialog` fecha, sem precisar de um efeito
  // extra para sincronizar os dois.
  const fotoLightbox = lightboxIndex !== null ? (fotos[lightboxIndex] ?? null) : null;

  return (
    <div className="flex flex-col gap-4 p-6">
      {statusConexao === 'reconectando' && (
        <output aria-live="polite" className="text-label text-muted-foreground">
          Reconectando...
        </output>
      )}

      {erro && (
        <p role="alert" className="text-body text-destructive">
          {erro === 'nao-encontrado' ? MENSAGEM_NAO_ENCONTRADO : MENSAGEM_ERRO}
        </p>
      )}

      {carregando && !produto && !erro && (
        <output className="text-body text-muted-foreground">Carregando produto...</output>
      )}

      {produto && (
        <Card>
          <CardHeader>
            <h1 className="text-heading-lg">{produto.nome}</h1>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex flex-col gap-1">
              <span className="text-label text-muted-foreground">
                {produto.codigo && <span className="font-mono">{produto.codigo}</span>}
                {produto.codigo && ' — '}
                {produto.categoria.nome}
              </span>
              <span className="text-body text-muted-foreground">
                {resumirDimensoes(produto.dimensoes)}
              </span>
              <IndicadorDisponibilidade disponivel={produto.disponivel} />
            </div>

            <div className="flex flex-col gap-2">
              <h2 className="text-heading-md">Quantidade por Estoque</h2>
              {produto.porEstoque.length === 0 ? (
                <p className="text-body text-muted-foreground">
                  {MENSAGEM_SEM_ESTOQUE_REGISTRADO}
                </p>
              ) : (
                <ul className="flex flex-col gap-1">
                  {produto.porEstoque.map((linha) => (
                    <li
                      key={linha.estoqueId}
                      className="text-body flex items-center justify-between gap-4"
                    >
                      <span>{linha.estoqueNome}</span>
                      <span className="flex items-center gap-3">
                        <span className="tabular-nums">{formatarQuantidade(linha.quantidade)}</span>
                        {podeRegistrarBaixa && (
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            aria-label={`Registrar Baixa em ${linha.estoqueNome}`}
                            onClick={() => {
                              setBaixaEstoque(linha);
                              setQuantidadeBaixa('');
                              setErroBaixa(null);
                            }}
                          >
                            Registrar Baixa
                          </Button>
                        )}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
              <p className="text-label text-muted-foreground">
                Total: {formatarQuantidade(produto.quantidadeTotal)}
              </p>
            </div>

            {fotos.length > 0 && (
              <div className="flex flex-col gap-2">
                <h2 className="text-heading-md">Fotos</h2>
                <div className="grid grid-cols-3 gap-2 sm:grid-cols-4">
                  {fotos.map((foto, index) => (
                    <button
                      key={foto.nome}
                      type="button"
                      onClick={() => setLightboxIndex(index)}
                      aria-label={`Ampliar foto ${index + 1} de ${fotos.length}`}
                      className="overflow-hidden rounded-md border border-border"
                    >
                      <img src={foto.objectUrl} alt="" className="h-24 w-24 object-cover" />
                    </button>
                  ))}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Lightbox (mesmo molde de CadastroProdutoSection.tsx): fechar (clique
          fora, Esc, ou o botão "Fechar") só muda `lightboxIndex` para `null`
          — nenhuma navegação, nenhum reload. */}
      <Dialog
        open={fotoLightbox !== null}
        onOpenChange={(open) => {
          if (!open) {
            setLightboxIndex(null);
          }
        }}
      >
        <DialogContent
          showCloseButton={false}
          className="flex w-screen h-screen max-w-none sm:!max-w-none translate-x-0 translate-y-0 top-0 left-0 items-center justify-center border-none bg-black/95 p-0"
        >
          <DialogTitle className="sr-only">Foto ampliada de {produto?.nome}</DialogTitle>
          <DialogClose className="absolute top-4 right-4 rounded-xs text-white opacity-90 ring-offset-background transition-opacity hover:opacity-100 focus:opacity-100 focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:outline-hidden">
            <XIcon className="size-6" />
            <span className="sr-only">Fechar</span>
          </DialogClose>
          {fotoLightbox && (
            <img
              src={fotoLightbox.objectUrl}
              alt=""
              className="max-h-full max-w-full object-contain"
            />
          )}
        </DialogContent>
      </Dialog>

      {/* Registrar Baixa (Story 5.1): controlado por `baixaEstoque` — `null`
          fecha o diálogo. Fechar enquanto o envio está em voo é ignorado
          (mesma defesa em profundidade do `enviandoBaixa` no botão). */}
      <Dialog
        open={baixaEstoque !== null}
        onOpenChange={(open) => {
          if (!open && !enviandoBaixa) {
            setBaixaEstoque(null);
            setErroBaixa(null);
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Registrar Baixa — {baixaEstoque?.estoqueNome}</DialogTitle>
          </DialogHeader>
          <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void confirmarBaixa();
            }}
          >
            <div className="flex flex-col gap-2">
              <Label htmlFor="baixa-quantidade">Quantidade</Label>
              <Input
                id="baixa-quantidade"
                type="number"
                inputMode="decimal"
                value={quantidadeBaixa}
                onChange={(event) => setQuantidadeBaixa(event.target.value)}
              />
            </div>
            {erroBaixa && (
              <p role="alert" className="text-body text-destructive">
                {erroBaixa}
              </p>
            )}
            <DialogFooter>
              <Button
                type="submit"
                disabled={enviandoBaixa || quantidadeBaixa.trim() === ''}
              >
                Confirmar
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export default ProdutoDetalhePage;
