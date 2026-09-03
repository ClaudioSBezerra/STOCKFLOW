import { createBrowserRouter, Navigate, RouterProvider, useLocation } from 'react-router-dom';
import { AppShell } from '@/components/shell/AppShell';
import { AuthProvider, useAuth } from '@/lib/auth';
import { CarrinhoProvider } from '@/lib/carrinho';
import { rankPapel } from '@/components/shell/nav-items';
import { PlaceholderPage } from '@/pages/PlaceholderPage';
import { CatalogoPage } from '@/pages/CatalogoPage';
import { ProdutoDetalhePage } from '@/pages/ProdutoDetalhePage';
import { CarrinhoPage } from '@/pages/CarrinhoPage';
import { PedidosPage } from '@/pages/PedidosPage';
import { CadastroPage } from '@/pages/CadastroPage';
import { VerificarEmailPage } from '@/pages/VerificarEmailPage';
import { LoginPage } from '@/pages/LoginPage';
import { EsqueciSenhaPage } from '@/pages/EsqueciSenhaPage';
import { RedefinirSenhaPage } from '@/pages/RedefinirSenhaPage';
import { ConfiguracoesPage } from '@/pages/ConfiguracoesPage';
import { EstoquesPage } from '@/pages/EstoquesPage';
import { NormalizacaoPage } from '@/pages/NormalizacaoPage';
import { AuthCallbackPage } from '@/pages/AuthCallbackPage';

/**
 * Rota raiz usa `AppShell` como layout. A raiz (`/`) deixou de ser
 * `PlaceholderPage` na Story 3.1 (spec-3-1): agora é `CatalogoPage` — aviso de
 * "busca em breve" (Epic 4) para qualquer papel, mais a seção de cadastro de
 * Produto para `almoxarife`+. Todo caminho não-raiz e não-listado continua
 * reaproveitando `PlaceholderPage` — nenhuma outra tela de produto real existe
 * ainda (ver spec-1-2). Itens de navegação do shell continuam clicáveis mesmo
 * sem uma rota "de verdade" atrás deles.
 *
 * `/cadastro`, `/verificar-email` (Story 1.3), `/login` (Story 1.4),
 * `/esqueci-senha` + `/redefinir-senha` (Story 1.6) e `/auth/callback`
 * (Story 1.9, retorno do login federado via Keycloak) são rotas públicas
 * irmãs da raiz, fora do `AppShell` e fora do
 * `RotaProtegida`. `/configuracoes` (Story 1.7, "Meu Perfil" + solicitação de
 * promoção de papel), `/estoques` (Story 2.1, tela "Locais": cadastro + lista
 * de locais de estoque, com gate de papel `almoxarife`+ na própria página),
 * `/produtos/:id` (Story 4.4, detalhe do Produto por Estoque com atualização
 * em tempo real, sem gate de papel próprio — `usuario`+), `/carrinho`
 * (Story 7.1, Carrinho de reserva — mesmo `usuario`+ sem gate de papel
 * próprio), `/pedidos` (Story 7.3, "Meus Pedidos": consulta dos Pedidos
 * próprios, mesmo `usuario`+ sem gate de papel próprio; aba "Fila" da Story
 * 7.4, spec-7-4, visível só a `almoxarife`+ — consulta de TODOS os Pedidos
 * da organização) e `/normalizacao`
 * (Story 6.1, "Inconsistências": detecção
 * dimensional sob demanda, mesmo gate de papel `almoxarife`+ de
 * `/estoques`) são rotas-filhas da raiz, dentro do `AppShell`/`RotaProtegida`.
 * A árvore do `AppShell` fica atrás do `RotaProtegida`
 * (Story 1.5): o `AuthProvider` faz o bootstrap silencioso da sessão ao
 * montar o app (silent refresh via cookie), e enquanto isso não resolve a
 * rota protegida mostra uma tela mínima de carregamento; conta anônima é
 * redirecionada para `/login`.
 *
 * `<CarrinhoProvider>` (Story 7.1) envolve `<RouterProvider>` por DENTRO de
 * `<AuthProvider>` — precisa de `useAuth()` (estado da sessão) para saber
 * quando buscar `GET /api/carrinho` pela primeira vez, e fica acima do
 * router porque tanto `AppShell` (cart-badge) quanto `CarrinhoPage`
 * consomem `useCarrinho()`.
 */

/**
 * Wrapper de rota que gateia a árvore autenticada — fail-closed: só o estado
 * EXPLÍCITO `autenticado` libera o shell; qualquer outra coisa que não seja
 * `carregando` (incl. um estado futuro inesperado) cai no redirect.
 * - `carregando`: tela mínima de carregamento (bootstrap em andamento).
 * - `autenticado`: renderiza o `AppShell`, sujeito ao gate de MFA abaixo.
 * - qualquer outro (`anonimo`, …): redireciona para `/login` (replace, para
 *   não empilhar histórico).
 *
 * Story 1.11 (MFA obrigatório para papéis administrativos): quando a sessão
 * é `origem==='senha'`, o papel alcança `gestor` na hierarquia e
 * `!mfaHabilitado`, a navegação normal fica bloqueada — todo caminho que não
 * seja `/configuracoes` redireciona para lá (replace), espelhando no cliente
 * o mesmo `403 MFA_SETUP_REQUIRED` que o servidor já aplicaria em
 * `middleware.RequireRole`. Itens do rail continuam visíveis (UX-DR22:
 * "bloqueando a navegação normal", não "escondendo") — só a navegação em si
 * é interceptada aqui, uma camada acima do shell.
 */
export function RotaProtegida() {
  const { estado, usuario } = useAuth();
  const location = useLocation();

  if (estado === 'carregando') {
    return (
      <output className="flex min-h-svh items-center justify-center text-muted-foreground">
        Carregando...
      </output>
    );
  }

  if (estado === 'autenticado') {
    const mfaPendente =
      usuario !== null &&
      usuario.origem === 'senha' &&
      rankPapel(usuario.papel) >= rankPapel('gestor') &&
      !usuario.mfaHabilitado;

    if (mfaPendente && location.pathname !== '/configuracoes') {
      return <Navigate to="/configuracoes" replace />;
    }

    return <AppShell />;
  }

  return <Navigate to="/login" replace />;
}

// Exportado só para os testes poderem resetar a localização entre casos
// (`router.navigate('/')`) — em runtime só o `<App />` abaixo o consome.
export const router = createBrowserRouter([
  {
    path: '/',
    element: <RotaProtegida />,
    children: [
      { index: true, element: <CatalogoPage /> },
      { path: 'produtos/:id', element: <ProdutoDetalhePage /> },
      { path: 'carrinho', element: <CarrinhoPage /> },
      { path: 'pedidos', element: <PedidosPage /> },
      { path: 'configuracoes', element: <ConfiguracoesPage /> },
      { path: 'estoques', element: <EstoquesPage /> },
      { path: 'normalizacao', element: <NormalizacaoPage /> },
      { path: '*', element: <PlaceholderPage /> },
    ],
  },
  { path: '/cadastro', element: <CadastroPage /> },
  { path: '/verificar-email', element: <VerificarEmailPage /> },
  { path: '/login', element: <LoginPage /> },
  { path: '/esqueci-senha', element: <EsqueciSenhaPage /> },
  { path: '/redefinir-senha', element: <RedefinirSenhaPage /> },
  { path: '/auth/callback', element: <AuthCallbackPage /> },
]);

function App() {
  return (
    <AuthProvider>
      <CarrinhoProvider>
        <RouterProvider router={router} />
      </CarrinhoProvider>
    </AuthProvider>
  );
}

export default App;
