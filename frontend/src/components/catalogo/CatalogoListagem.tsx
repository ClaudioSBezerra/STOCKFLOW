import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { ChevronDown, ChevronRight, LayoutGrid, Table2 } from 'lucide-react';
import { Button } from '@/components/ui/button';
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
 */

interface CategoriaCatalogo {
  id: string;
  codigo: string;
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

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

export function CatalogoListagem() {
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

  const [itens, setItens] = useState<CatalogoItem[]>([]);
  const [grupos, setGrupos] = useState<CatalogoGrupo[]>([]);
  const [paginacao, setPaginacao] = useState<Paginacao | null>(null);
  const [carregando, setCarregando] = useState(true);
  const [erro, setErro] = useState(false);
  const [expandidos, setExpandidos] = useState<Set<string>>(() => new Set());

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

  const carregar = useCallback(async (modoAtual: Modo, paginaAtual: number) => {
    const seq = ++seqRef.current;
    setCarregando(true);
    setErro(false);
    try {
      const agrupar = modoAtual === 'tabela';
      const res = await fetch(
        `/api/produtos/catalogo?agrupar=${agrupar}&pagina=${paginaAtual}`,
        { headers: authHeaders() },
      );
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

  useEffect(() => {
    void (async () => {
      await carregar(modo, pagina);
    })();
  }, [carregar, modo, pagina]);

  function alternarModo(novoModo: Modo) {
    if (novoModo === modo) {
      return;
    }
    setModo(novoModo);
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
        </div>
      )}

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
