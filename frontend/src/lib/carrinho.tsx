import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { toast } from 'sonner';
import { getAccessToken } from '@/lib/session';
import { useAuth } from '@/lib/auth';

/**
 * Carrinho de reserva (Story 7.1, spec-7-1). `CarrinhoProvider` envolve
 * `<RouterProvider>` por DENTRO de `<AuthProvider>` (App.tsx) — molde de
 * Context+Provider de `lib/auth.tsx` — e é o único dono do estado do
 * carrinho no frontend: `itens` (consumido por `CarrinhoPage`) e `count`
 * (consumido pelo `cart-badge` do shell, `AppShell.tsx`).
 *
 * Toda operação é escopada ao usuário da SESSÃO no servidor (Always,
 * spec-7-1) — este módulo nunca envia um `usuarioId`, só chama
 * `/api/carrinho*` com o `Authorization: Bearer` corrente.
 *
 * Sem canal SSE (Never, spec-7-1): `refresh` é o único caminho de
 * atualização, chamado (a) quando a sessão fica `autenticado` (estado
 * inicial do badge, sem precisar visitar `/carrinho`), (b) depois de
 * `adicionarItem`/`removerItem` terem sucesso, e (c) por `CarrinhoPage` ao
 * montar. `GET /api/carrinho` faz limpeza preguiçosa no servidor
 * (`removidos`) — `refresh` traduz cada linha removida num
 * `toast.info` automático (AC2: "esse item some da lista automaticamente e
 * um aviso explica o motivo"), então qualquer chamador de `refresh` já
 * herda esse aviso, não só `CarrinhoPage`.
 */
export interface ItemCarrinho {
  produtoId: string;
  produtoNome: string;
  estoqueId: string;
  estoqueNome: string;
  quantidade: number;
}

export type MotivoItemCarrinhoRemovido = 'produto_removido' | 'estoque_excluido';

export interface ItemCarrinhoRemovido {
  produtoId: string;
  produtoNome: string;
  estoqueId: string;
  estoqueNome: string | null;
  motivo: MotivoItemCarrinhoRemovido;
}

export type ResultadoOperacaoCarrinho = { ok: true } | { ok: false; mensagem: string };

interface CarrinhoContextValue {
  /** Itens ativos do carrinho — consumido por CarrinhoPage. */
  itens: ItemCarrinho[];
  /**
   * Contagem para o `cart-badge` (UX-DR5: nunca "0" — o chamador decide
   * `count > 0` antes de renderizar o badge, este valor não esconde nada
   * sozinho).
   */
  count: number;
  /** `true` enquanto a primeira carga (ou um refresh) está em voo. */
  carregando: boolean;
  /**
   * `true` quando o `refresh` mais recente falhou (resposta não-ok ou
   * exceção de rede) — distingue "carrinho vazio de verdade" de "não
   * conseguimos carregar o carrinho agora" (mesmo convênio de três estados —
   * carregando/erro/vazio — de CatalogoListagem.tsx). Volta a `false` assim
   * que um `refresh` subsequente tiver sucesso.
   */
  erro: boolean;
  /** Rebusca `GET /api/carrinho` — nunca lança; falha marca `erro=true` em vez de deixar `itens` vazio passar por "carrinho vazio". */
  refresh: () => Promise<void>;
  /** `POST /api/carrinho/itens` + refresh em caso de sucesso. */
  adicionarItem: (produtoId: string, estoqueId: string, quantidade: number) => Promise<ResultadoOperacaoCarrinho>;
  /** `DELETE /api/carrinho/itens/{produtoId}/{estoqueId}` + refresh em caso de sucesso. */
  removerItem: (produtoId: string, estoqueId: string) => Promise<ResultadoOperacaoCarrinho>;
}

const CarrinhoContext = createContext<CarrinhoContextValue | null>(null);

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_ADICIONAR =
  'Não foi possível adicionar o item ao carrinho agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_REMOVER =
  'Não foi possível remover o item do carrinho agora. Tente novamente em instantes.';

/**
 * Mensagem de aviso por item removido preguiçosamente (AC2, spec-7-1).
 * `switch` exaustivo sobre `MotivoItemCarrinhoRemovido` (em vez de
 * `if/else` com fallback implícito) — um `motivo` desconhecido/futuro cai no
 * `default` genérico, nunca é rotulado silenciosamente como
 * `estoque_excluido`.
 */
export function mensagemItemCarrinhoRemovido(item: ItemCarrinhoRemovido): string {
  switch (item.motivo) {
    case 'produto_removido':
      return `"${item.produtoNome}" foi removido do carrinho: o produto não existe mais.`;
    case 'estoque_excluido':
      return `"${item.produtoNome}" foi removido do carrinho: o estoque selecionado foi excluído.`;
    default:
      return `"${item.produtoNome}" foi removido do carrinho.`;
  }
}

export function CarrinhoProvider({ children }: { children: ReactNode }) {
  const { estado } = useAuth();
  const [itens, setItens] = useState<ItemCarrinho[]>([]);
  const [carregando, setCarregando] = useState(false);
  const [erro, setErro] = useState(false);
  // Contador de geração de requisição: cada `refresh()` incrementa e captura
  // o próprio número ANTES do `await`. Cenário real (estação compartilhada
  // de almoxarifado): o Usuário A está autenticado com um `refresh()` em
  // voo; A faz logout e B faz login imediatamente na MESMA aba (troca de
  // conta em memória, sem reload) — se a resposta lenta e obsoleta de A
  // resolver DEPOIS que o `refresh()` do próprio B (mais rápido) já aplicou
  // o estado correto, a resposta de A sobrescreveria o estado de B com
  // dados de outro usuário. Comparar `geracaoRef.current` com a geração
  // capturada por ESTA chamada, depois do `await`, descarta a resposta
  // inteira (nenhum `setItens`/`setCarregando`/`setErro`/toast) sempre que
  // um `refresh()` mais novo já tiver começado.
  const geracaoRef = useRef(0);

  const refresh = useCallback(async () => {
    const minhaGeracao = ++geracaoRef.current;
    setCarregando(true);
    try {
      const res = await fetch('/api/carrinho', { headers: authHeaders() });
      if (!res.ok) {
        if (minhaGeracao === geracaoRef.current) {
          setErro(true);
        }
        return;
      }
      const body = (await res.json()) as {
        itens: ItemCarrinho[];
        removidos: ItemCarrinhoRemovido[];
      };
      if (minhaGeracao !== geracaoRef.current) {
        return; // resposta obsoleta: um refresh() mais novo já aplicou seu resultado
      }
      setItens(body.itens);
      setErro(false);
      for (const removido of body.removidos) {
        toast.info(mensagemItemCarrinhoRemovido(removido));
      }
    } catch {
      // Falha de rede/500: marca `erro=true` (em vez de deixar `itens`
      // vazio passar por "carrinho vazio") — mesmo convênio de três estados
      // de CatalogoListagem.tsx.
      if (minhaGeracao === geracaoRef.current) {
        setErro(true);
      }
    } finally {
      if (minhaGeracao === geracaoRef.current) {
        setCarregando(false);
      }
    }
  }, []);

  // Carrega o carrinho assim que a sessão fica autenticada — sem isso o
  // cart-badge só apareceria depois da primeira visita a /carrinho ou da
  // primeira adição nesta aba. Sessão anônima nunca renderiza uma superfície
  // que leia `itens`/`count` (RotaProtegida redireciona para /login antes),
  // então não há necessidade de zerar o estado local só por sair —
  // `refresh` já substitui `itens` pelo carrinho certo assim que uma
  // próxima conta autentica nesta aba.
  useEffect(() => {
    if (estado !== 'autenticado') {
      return;
    }
    // `queueMicrotask` (em vez de `void refresh()` direto) evita disparar o
    // `setCarregando(true)` síncrono de `refresh` na MESMA passada de render
    // do efeito — mesmo cuidado de bootstrap() em lib/auth.tsx, que também
    // nunca chama um setState antes do primeiro `await`.
    queueMicrotask(() => {
      void refresh();
    });
  }, [estado, refresh]);

  const adicionarItem = useCallback(
    async (produtoId: string, estoqueId: string, quantidade: number): Promise<ResultadoOperacaoCarrinho> => {
      try {
        const res = await fetch('/api/carrinho/itens', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json', ...authHeaders() },
          body: JSON.stringify({ produtoId, estoqueId, quantidade }),
        });
        if (!res.ok) {
          const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
          return { ok: false, mensagem: body.error?.message ?? MENSAGEM_ERRO_ADICIONAR };
        }
        await refresh();
        return { ok: true };
      } catch {
        return { ok: false, mensagem: MENSAGEM_ERRO_ADICIONAR };
      }
    },
    [refresh],
  );

  const removerItem = useCallback(
    async (produtoId: string, estoqueId: string): Promise<ResultadoOperacaoCarrinho> => {
      try {
        const res = await fetch(
          `/api/carrinho/itens/${produtoId}/${estoqueId}`,
          { method: 'DELETE', headers: authHeaders() },
        );
        if (!(res.status === 204 || res.ok)) {
          const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
          return { ok: false, mensagem: body.error?.message ?? MENSAGEM_ERRO_REMOVER };
        }
        await refresh();
        return { ok: true };
      } catch {
        return { ok: false, mensagem: MENSAGEM_ERRO_REMOVER };
      }
    },
    [refresh],
  );

  const value = useMemo<CarrinhoContextValue>(
    () => ({ itens, count: itens.length, carregando, erro, refresh, adicionarItem, removerItem }),
    [itens, carregando, erro, refresh, adicionarItem, removerItem],
  );

  return <CarrinhoContext.Provider value={value}>{children}</CarrinhoContext.Provider>;
}

export function useCarrinho(): CarrinhoContextValue {
  const ctx = useContext(CarrinhoContext);
  if (!ctx) {
    throw new Error('useCarrinho precisa ser usado dentro de <CarrinhoProvider>');
  }
  return ctx;
}
