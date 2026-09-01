import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { ChevronDown, ChevronRight, LayoutGrid, Table2 } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { getAccessToken } from '@/lib/session';
import {
  formatarQuantidade,
  IndicadorDisponibilidade,
  resumirDimensoes,
  type Dimensoes,
} from './formatacao';

/**
 * Listagem do Catálogo (Story 4.3, spec-4-3, FR-6) — renderizada em
 * `CatalogoPage` logo abaixo de `BuscaCatalogo`, para qualquer papel
 * (`usuario`+): NÃO depende do gate `podeCadastrar` da página.
 *
 * Busca `GET /api/produtos/catalogo?agrupar=<>&pagina=<>` no mount e a cada
 * troca de modo/página. Dois modos:
 *  - grade (`agrupar=false`): um card por Produto — nome, código em
 *    `JetBrains Mono` quando presente, nome da categoria e o indicador de
 *    disponibilidade (ícone + texto, nunca só cor — UX-DR10).
 *  - tabela (`agrupar=true`): Produtos com mesmo nome + mesmas 5 dimensões
 *    estruturadas colapsados numa linha; expandir a linha (`aria-expanded`)
 *    revela a quantidade discriminada por Estoque (ou "Sem quantidade
 *    registrada por estoque." quando `porEstoque` é vazio).
 *
 * O alternador grade/tabela só é renderizado quando
 * `window.matchMedia('(min-width: 768px)').matches` — abaixo de 768px o modo
 * é sempre grade e o alternador some (UX-DR16). O componente escuta a
 * mudança de viewport e volta para grade se ela encolher abaixo de 768px.
 *
 * Paginação sempre numérica, nunca scroll infinito. Estados: carregando,
 * erro (`role="alert"`) e vazio (`total === 0`).
 *
 * Cada card da grade (Story 4.4, spec-4-4) envolve seu conteúdo num
 * `<Link to={`/produtos/${id}`}>`, navegando para o detalhe do Produto. A
 * tabela agrupada NÃO ganha navegação: um grupo pode conter vários Produtos
 * distintos (mesmo nome+dimensões, categorias diferentes), sem um único `id`
 * de destino — a discriminação por Estoque ao expandir já cobre essa
 * necessidade sem depender do detalhe.
 *
 * Filtros combináveis (Story 4.2, spec-4-2): prop `termo` (já trimado,
 * repassado por `CatalogoPage` a partir de `BuscaCatalogo`, debounçado lá) +
 * 3 controles próprios — `<Select>` de categoria, `<Select>` de Estoque
 * (opções carregadas uma vez no mount de `GET /api/categorias`/`GET
 * /api/estoques`; falha de qualquer um dos dois degrada silenciosamente para
 * só a opção sentinela "Todas"/"Todos", nunca bloqueia a listagem em si) e
 * `Checkbox` "Com estoque disponível". Os 4 filtros (`q`, `categoriaId`,
 * `estoqueId`, `comEstoque`) combinam sempre por E lógico na query string.
 * Qualquer mudança de filtro OU do `termo` (prop) volta `pagina` para 1 e
 * redispara o `fetch` (mesmo padrão de `alternarModo`).
 *
 * Exportação para Excel (Story 4.6, spec-4-6, FR-30): prop opcional
 * `podeExportar` (default `false`, calculada por `CatalogoPage` a partir do
 * papel do usuário). Botão "Exportar" ao lado do alternador grade/tabela,
 * visível só quando `podeExportar && modo === 'tabela'` — a exportação é
 * sempre da tabela agrupada com os filtros ativos, nunca da grade. Reusa a
 * mesma serialização de filtros de `carregar` (helper `queryFiltros`, sem
 * `agrupar`/`pagina` — a exportação sempre traz o conjunto filtrado
 * completo) contra `GET /api/produtos/catalogo/exportar`; sucesso baixa
 * `catalogo.xlsx` via `Blob`/`URL.createObjectURL`; falha (rede ou resposta
 * não-OK) mostra `toast.error` (mesmo padrão de `ScannerProdutoFab`). O botão
 * fica desabilitado ("Exportando...") enquanto a requisição está em voo.
 */

const MENSAGEM_ERRO_EXPORTACAO = 'Não foi possível exportar o catálogo. Tente novamente em instantes.';

interface CategoriaCatalogo {
  id: string;
  codigo: string;
  nome: string;
}

interface EstoqueFiltro {
  id: string;
  nome: string;
}

interface CatalogoItem {
  id: string;
  nome: string;
  codigo: string | null;
  categoria: CategoriaCatalogo;
  dimensoes: Dimensoes;
  quantidadeTotal: number;
  disponivel: boolean;
}

interface EstoqueQuantidade {
  estoqueId: string;
  estoqueNome: string;
  quantidade: number;
}

interface CatalogoGrupo {
  chave: string;
  nome: string;
  dimensoes: Dimensoes;
  quantidadeTotal: number;
  disponivel: boolean;
  porEstoque: EstoqueQuantidade[];
}

interface Paginacao {
  pagina: number;
  tamanho: number;
  total: number;
  totalPaginas: number;
}

type Modo = 'grade' | 'tabela';

const MEDIA_QUERY = '(min-width: 768px)';

const MENSAGEM_ERRO = 'Não foi possível carregar o catálogo. Tente novamente em instantes.';
const MENSAGEM_VAZIO = 'Nenhum produto no catálogo.';
const MENSAGEM_SEM_ESTOQUE_REGISTRADO = 'Sem quantidade registrada por estoque.';

// SEM_CATEGORIA/SEM_ESTOQUE são os valores sentinela das opções "Todas as
// categorias"/"Todos os Estoques" — Radix `Select.Item` proíbe `value=""`
// (usado internamente para representar "nada selecionado"), mesmo padrão
// `SEM_TEMPLATE` de CadastroProdutoSection.
const SEM_CATEGORIA = '__todas-categorias__';
const SEM_ESTOQUE = '__todos-estoques__';

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

interface FiltrosAtivos {
  categoriaId: string;
  estoqueId: string;
  comEstoque: boolean;
  termo: string;
}

// queryFiltros monta a fatia comum de query string dos 4 filtros
// combináveis (`q`/`categoriaId`/`estoqueId`/`comEstoque`), SEM
// `agrupar`/`pagina` — reusada por `carregar` (que prefixa `agrupar=&
// pagina=`) e por `aoExportar` (que não tem nenhum dos dois, Story 4.6,
// spec-4-6: a exportação nunca é de uma página, sempre o filtro completo).
// Devolve os pares já unidos por `&`, sem `&`/`?` líder — cabe ao chamador
// prefixar o que precisar.
function queryFiltros(filtros: FiltrosAtivos): string {
  const partes: string[] = [];
  const termoTrimado = filtros.termo.trim();
  if (termoTrimado !== '') {
    partes.push(`q=${encodeURIComponent(termoTrimado)}`);
  }
  if (filtros.categoriaId !== '') {
    partes.push(`categoriaId=${encodeURIComponent(filtros.categoriaId)}`);
  }
  if (filtros.estoqueId !== '') {
    partes.push(`estoqueId=${encodeURIComponent(filtros.estoqueId)}`);
  }
  if (filtros.comEstoque) {
    partes.push('comEstoque=true');
  }
  return partes.join('&');
}

interface CatalogoListagemProps {
  termo?: string;
  podeExportar?: boolean;
}

export function CatalogoListagem({ termo = '', podeExportar = false }: CatalogoListagemProps = {}) {
  const [modo, setModo] = useState<Modo>('grade');
  // Estado inicial derivado direto do matchMedia (não de um setState no
  // efeito): o alternador só existe em >=768px. O efeito abaixo só mantém
  // esse valor sincronizado quando a viewport muda em runtime.
  const [podeAlternar, setPodeAlternar] = useState(
    () =>
      typeof window !== 'undefined' &&
      typeof window.matchMedia === 'function' &&
      window.matchMedia(MEDIA_QUERY).matches,
  );
  const [pagina, setPagina] = useState(1);

  // Filtros (Story 4.2, spec-4-2): '' = sem filtro de categoria/Estoque
  // (opção sentinela "Todas"/"Todos" selecionada).
  const [categoriaId, setCategoriaId] = useState('');
  const [estoqueId, setEstoqueId] = useState('');
  const [comEstoque, setComEstoque] = useState(false);
  const [categorias, setCategorias] = useState<CategoriaCatalogo[]>([]);
  const [estoques, setEstoques] = useState<EstoqueFiltro[]>([]);

  const [itens, setItens] = useState<CatalogoItem[]>([]);
  const [grupos, setGrupos] = useState<CatalogoGrupo[]>([]);
  const [paginacao, setPaginacao] = useState<Paginacao | null>(null);
  const [carregando, setCarregando] = useState(true);
  const [erro, setErro] = useState(false);
  const [expandidos, setExpandidos] = useState<Set<string>>(() => new Set());

  // exportando (Story 4.6, spec-4-6): true enquanto a requisição de
  // exportação está em voo — desabilita o botão "Exportar" e troca seu
  // rótulo para "Exportando...".
  const [exportando, setExportando] = useState(false);

  // Contador de sequência: respostas de uma busca antiga que chegam depois de
  // uma nova são descartadas (mesma guarda de LogAcessoSection/BuscaCatalogo).
  const seqRef = useRef(0);

  // Detecção de viewport: o alternador só existe em >=768px; se a viewport
  // encolher abaixo disso, o modo volta para grade (UX-DR16).
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') {
      return;
    }
    const mql = window.matchMedia(MEDIA_QUERY);

    function aoMudarViewport(evento: MediaQueryListEvent) {
      setPodeAlternar(evento.matches);
      if (!evento.matches) {
        setModo('grade');
        setPagina(1);
      }
    }

    mql.addEventListener?.('change', aoMudarViewport);
    return () => mql.removeEventListener?.('change', aoMudarViewport);
  }, []);

  // carregar recebe modo/página/filtros como parâmetros explícitos (não lidos
  // de closure) — mesmo cuidado contra stale closure já usado neste
  // componente; os efeitos abaixo passam sempre os valores atuais de state.
  const carregar = useCallback(async (modoAtual: Modo, paginaAtual: number, filtrosAtuais: FiltrosAtivos) => {
    const seq = ++seqRef.current;
    setCarregando(true);
    setErro(false);
    try {
      const agrupar = modoAtual === 'tabela';
      let query = `agrupar=${agrupar}&pagina=${paginaAtual}`;
      const extras = queryFiltros(filtrosAtuais);
      if (extras !== '') {
        query += `&${extras}`;
      }
      const res = await fetch(`/api/produtos/catalogo?${query}`, { headers: authHeaders() });
      if (seq !== seqRef.current) {
        return;
      }
      if (!res.ok) {
        setErro(true);
        return;
      }
      const data = (await res.json()) as {
        produtos?: CatalogoItem[];
        grupos?: CatalogoGrupo[];
        paginacao?: Paginacao;
      };
      if (seq !== seqRef.current) {
        return;
      }
      setPaginacao(data.paginacao ?? null);
      if (agrupar) {
        setGrupos(Array.isArray(data.grupos) ? data.grupos : []);
        setItens([]);
      } else {
        setItens(Array.isArray(data.produtos) ? data.produtos : []);
        setGrupos([]);
      }
      setExpandidos(new Set());
    } catch {
      if (seq === seqRef.current) {
        setErro(true);
      }
    } finally {
      if (seq === seqRef.current) {
        setCarregando(false);
      }
    }
  }, []);

  // Ajusta `pagina` para 1 quando `termo` (prop) muda — DURANTE o render,
  // não num `useEffect` (padrão recomendado do React para "ajustar estado
  // quando uma prop muda": https://react.dev/learn/you-might-not-need-an-effect).
  // `termo` chega via prop (não por um setState local como os outros
  // filtros), então não dá para resetar `pagina` no mesmo evento que o
  // alterou (mesmo padrão batched de `alternarModo`); ajustar aqui, antes do
  // commit, evita tanto um efeito dedicado quanto uma chamada extra a
  // `carregar` com a combinação errada (página velha + termo novo) que um
  // `useEffect([termo])` separado produziria.
  const [termoAnterior, setTermoAnterior] = useState(termo);
  if (termo !== termoAnterior) {
    setTermoAnterior(termo);
    setPagina(1);
  }

  useEffect(() => {
    void (async () => {
      await carregar(modo, pagina, { categoriaId, estoqueId, comEstoque, termo });
    })();
  }, [carregar, modo, pagina, categoriaId, estoqueId, comEstoque, termo]);

  // Carrega as listas de categoria/Estoque uma vez no mount para popular os
  // dois `<Select>` de filtro (mesmo padrão de
  // CadastroProdutoSection.carregarListas) — falha de qualquer um dos dois
  // degrada silenciosamente SÓ NAQUELE campo (o `<Select>` correspondente
  // mostra só a opção sentinela "Todas"/"Todos"), nunca bloqueia a listagem
  // em si NEM o outro `<Select>`. As duas buscas usam `try/catch`
  // independentes (não `Promise.all` num único bloco) de propósito: com
  // `Promise.all`, uma falha de rede isolada em SÓ UM dos dois endpoints
  // rejeitaria a promise combinada e derrubaria os DOIS `<Select>` para a
  // opção sentinela, mesmo quando o outro respondeu com sucesso — achado
  // pelo Blind Hunter na revisão desta story.
  useEffect(() => {
    void (async () => {
      try {
        const res = await fetch('/api/categorias', { headers: authHeaders() });
        if (res.ok) {
          const body = (await res.json()) as { categorias?: CategoriaCatalogo[] };
          setCategorias(Array.isArray(body.categorias) ? body.categorias : []);
        }
      } catch {
        // Falha de rede isolada em categorias — degrada silenciosamente
        // (Always, spec-4-2): a listagem e o Select de Estoque não dependem
        // dela.
      }
    })();
  }, []);

  useEffect(() => {
    void (async () => {
      try {
        const res = await fetch('/api/estoques', { headers: authHeaders() });
        if (res.ok) {
          const body = (await res.json()) as { estoques?: EstoqueFiltro[] };
          setEstoques(Array.isArray(body.estoques) ? body.estoques : []);
        }
      } catch {
        // Falha de rede isolada em Estoques — degrada silenciosamente
        // (Always, spec-4-2): a listagem e o Select de categoria não
        // dependem dela.
      }
    })();
  }, []);

  function alternarModo(novoModo: Modo) {
    if (novoModo === modo) {
      return;
    }
    setModo(novoModo);
    setPagina(1);
  }

  // aoExportar (Story 4.6, spec-4-6, FR-30): baixa `catalogo.xlsx` com os
  // MESMOS filtros ativos da tabela — reusa `queryFiltros`, sem
  // `agrupar`/`pagina` (a exportação sempre traz o filtro completo, nunca
  // uma página). Sucesso: `Blob` da resposta -> `URL.createObjectURL` -> um
  // `<a download>` criado, clicado e removido -> `URL.revokeObjectURL`.
  // Falha (resposta não-OK ou exceção de rede) -> `toast.error`, mesmo
  // padrão de `ScannerProdutoFab`.
  async function aoExportar() {
    setExportando(true);
    try {
      const extras = queryFiltros({ categoriaId, estoqueId, comEstoque, termo });
      const url = `/api/produtos/catalogo/exportar${extras !== '' ? `?${extras}` : ''}`;
      const res = await fetch(url, { headers: authHeaders() });
      if (!res.ok) {
        toast.error(MENSAGEM_ERRO_EXPORTACAO);
        return;
      }
      const blob = await res.blob();
      const href = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = href;
      link.download = 'catalogo.xlsx';
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(href);
    } catch {
      toast.error(MENSAGEM_ERRO_EXPORTACAO);
    } finally {
      setExportando(false);
    }
  }

  function aoMudarCategoria(valor: string) {
    setCategoriaId(valor === SEM_CATEGORIA ? '' : valor);
    setPagina(1);
  }

  function aoMudarEstoque(valor: string) {
    setEstoqueId(valor === SEM_ESTOQUE ? '' : valor);
    setPagina(1);
  }

  function aoMudarComEstoque(estado: boolean | 'indeterminate') {
    setComEstoque(estado === true);
    setPagina(1);
  }

  function alternarExpandido(chave: string) {
    setExpandidos((atual) => {
      const proximo = new Set(atual);
      if (proximo.has(chave)) {
        proximo.delete(chave);
      } else {
        proximo.add(chave);
      }
      return proximo;
    });
  }

  const total = paginacao?.total ?? 0;
  const totalPaginas = paginacao?.totalPaginas ?? 0;
  const vazio = !carregando && !erro && total === 0;
  const mostrarPaginacao = !erro && !vazio && totalPaginas > 1;

  return (
    <section className="flex flex-col gap-4" aria-label="Catálogo de produtos">
      {podeAlternar && (
        <div className="flex gap-2">
          <Button
            type="button"
            variant={modo === 'grade' ? 'default' : 'outline'}
            aria-pressed={modo === 'grade'}
            onClick={() => alternarModo('grade')}
            className="min-h-touch-target-min"
          >
            <LayoutGrid aria-hidden="true" className="h-4 w-4" />
            Grade
          </Button>
          <Button
            type="button"
            variant={modo === 'tabela' ? 'default' : 'outline'}
            aria-pressed={modo === 'tabela'}
            onClick={() => alternarModo('tabela')}
            className="min-h-touch-target-min"
          >
            <Table2 aria-hidden="true" className="h-4 w-4" />
            Tabela
          </Button>
          {podeExportar && modo === 'tabela' && (
            <Button
              type="button"
              variant="outline"
              disabled={exportando}
              onClick={() => void aoExportar()}
              className="min-h-touch-target-min"
            >
              {exportando ? 'Exportando...' : 'Exportar'}
            </Button>
          )}
        </div>
      )}

      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1">
          <Label htmlFor="catalogo-filtro-categoria" className="text-label text-muted-foreground">
            Categoria
          </Label>
          <Select value={categoriaId === '' ? SEM_CATEGORIA : categoriaId} onValueChange={aoMudarCategoria}>
            <SelectTrigger id="catalogo-filtro-categoria" className="min-h-touch-target-min">
              <SelectValue placeholder="Todas as categorias" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={SEM_CATEGORIA}>Todas as categorias</SelectItem>
              {categorias.map((categoria) => (
                <SelectItem key={categoria.id} value={categoria.id}>
                  {categoria.nome}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex flex-col gap-1">
          <Label htmlFor="catalogo-filtro-estoque" className="text-label text-muted-foreground">
            Estoque
          </Label>
          <Select value={estoqueId === '' ? SEM_ESTOQUE : estoqueId} onValueChange={aoMudarEstoque}>
            <SelectTrigger id="catalogo-filtro-estoque" className="min-h-touch-target-min">
              <SelectValue placeholder="Todos os Estoques" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={SEM_ESTOQUE}>Todos os Estoques</SelectItem>
              {estoques.map((estoque) => (
                <SelectItem key={estoque.id} value={estoque.id}>
                  {estoque.nome}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex min-h-touch-target-min items-center gap-2">
          <Checkbox
            id="catalogo-filtro-com-estoque"
            checked={comEstoque}
            onCheckedChange={aoMudarComEstoque}
          />
          <Label htmlFor="catalogo-filtro-com-estoque">Com estoque disponível</Label>
        </div>
      </div>

      {erro && (
        <p role="alert" className="text-body text-destructive">
          {MENSAGEM_ERRO}
        </p>
      )}

      {carregando && (
        <output className="text-body text-muted-foreground">Carregando catálogo...</output>
      )}

      {vazio && <p className="text-body text-muted-foreground">{MENSAGEM_VAZIO}</p>}

      {!carregando && !erro && !vazio && modo === 'grade' && (
        <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {itens.map((item) => (
            <li key={item.id}>
              <Link
                to={`/produtos/${item.id}`}
                className="min-h-touch-target-min flex flex-col gap-2 rounded-md border border-border p-3"
              >
                <span className="text-body font-medium">{item.nome}</span>
                <span className="text-label text-muted-foreground">
                  {item.codigo && <span className="font-mono">{item.codigo}</span>}
                  {item.codigo && ' — '}
                  {item.categoria.nome}
                </span>
                <IndicadorDisponibilidade disponivel={item.disponivel} />
              </Link>
            </li>
          ))}
        </ul>
      )}

      {!carregando && !erro && !vazio && modo === 'tabela' && (
        <div className="overflow-x-auto">
          <table className="w-full text-body">
            <thead>
              <tr className="text-label text-muted-foreground">
                <th className="py-2 pr-4 text-left font-medium" scope="col">
                  <span className="sr-only">Expandir</span>
                </th>
                <th className="py-2 pr-4 text-left font-medium" scope="col">
                  Produto
                </th>
                <th className="py-2 pr-4 text-left font-medium" scope="col">
                  Dimensões
                </th>
                <th className="py-2 pr-4 text-right font-medium" scope="col">
                  Quantidade
                </th>
                <th className="py-2 text-left font-medium" scope="col">
                  Disponibilidade
                </th>
              </tr>
            </thead>
            <tbody>
              {grupos.map((grupo) => {
                const expandido = expandidos.has(grupo.chave);
                return (
                  <FragmentLinhaGrupo
                    key={grupo.chave}
                    grupo={grupo}
                    expandido={expandido}
                    aoAlternar={() => alternarExpandido(grupo.chave)}
                  />
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {mostrarPaginacao && paginacao && (
        <nav className="flex items-center gap-3" aria-label="Paginação do catálogo">
          <Button
            type="button"
            variant="outline"
            className="min-h-touch-target-min"
            disabled={paginacao.pagina <= 1 || carregando}
            onClick={() => setPagina((p) => Math.max(1, p - 1))}
          >
            Anterior
          </Button>
          <span className="text-label text-muted-foreground" aria-live="polite">
            Página {paginacao.pagina} de {Math.max(1, totalPaginas)}
          </span>
          <Button
            type="button"
            variant="outline"
            className="min-h-touch-target-min"
            disabled={paginacao.pagina >= totalPaginas || carregando}
            onClick={() => setPagina((p) => Math.min(totalPaginas, p + 1))}
          >
            Próxima
          </Button>
        </nav>
      )}
    </section>
  );
}

function FragmentLinhaGrupo({
  grupo,
  expandido,
  aoAlternar,
}: {
  grupo: CatalogoGrupo;
  expandido: boolean;
  aoAlternar: () => void;
}) {
  const Chevron = expandido ? ChevronDown : ChevronRight;
  return (
    <>
      <tr className="border-t border-border">
        <td className="py-2 pr-4">
          <button
            type="button"
            aria-expanded={expandido}
            onClick={aoAlternar}
            className="min-h-touch-target-min inline-flex items-center justify-center"
          >
            <Chevron aria-hidden="true" className="h-4 w-4" />
            <span className="sr-only">
              {expandido ? 'Recolher' : 'Expandir'} {grupo.nome}
            </span>
          </button>
        </td>
        <td className="py-2 pr-4 font-medium">{grupo.nome}</td>
        <td className="py-2 pr-4 text-muted-foreground">{resumirDimensoes(grupo.dimensoes)}</td>
        <td className="py-2 pr-4 text-right tabular-nums">
          {formatarQuantidade(grupo.quantidadeTotal)}
        </td>
        <td className="py-2">
          <IndicadorDisponibilidade disponivel={grupo.disponivel} />
        </td>
      </tr>
      {expandido && (
        <tr className="border-t border-border/50 bg-muted/40">
          <td colSpan={5} className="py-2 pr-4 pl-10">
            {grupo.porEstoque.length === 0 ? (
              <span className="text-label text-muted-foreground">
                {MENSAGEM_SEM_ESTOQUE_REGISTRADO}
              </span>
            ) : (
              <ul className="flex flex-col gap-1">
                {grupo.porEstoque.map((linha) => (
                  <li key={linha.estoqueId} className="text-label flex justify-between gap-4">
                    <span>{linha.estoqueNome}</span>
                    <span className="tabular-nums">{formatarQuantidade(linha.quantidade)}</span>
                  </li>
                ))}
              </ul>
            )}
          </td>
        </tr>
      )}
    </>
  );
}

export default CatalogoListagem;
