import { createBrowserRouter, Navigate, RouterProvider, useLocation } from 'react-router-dom';
import { AppShell } from '@/components/shell/AppShell';
import { AuthProvider, useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { PlaceholderPage } from '@/pages/PlaceholderPage';
import { CadastroPage } from '@/pages/CadastroPage';
import { VerificarEmailPage } from '@/pages/VerificarEmailPage';
import { LoginPage } from '@/pages/LoginPage';
import { EsqueciSenhaPage } from '@/pages/EsqueciSenhaPage';
import { RedefinirSenhaPage } from '@/pages/RedefinirSenhaPage';
import { ConfiguracoesPage } from '@/pages/ConfiguracoesPage';
import { AuthCallbackPage } from '@/pages/AuthCallbackPage';

/**
 * Rota raiz usa `AppShell` como layout; todo caminho não-raiz reaproveita a
 * mesma página placeholder — nenhuma tela de produto real existe ainda
 * (ver spec-1-2). Itens de navegação do shell continuam clicáveis mesmo sem
 * uma rota "de verdade" atrás deles.
 *
 * `/cadastro`, `/verificar-email` (Story 1.3), `/login` (Story 1.4),
 * `/esqueci-senha` + `/redefinir-senha` (Story 1.6) e `/auth/callback`
 * (Story 1.9, retorno do login federado via Keycloak) são rotas públicas
 * irmãs da raiz, fora do `AppShell` e fora do
 * `RotaProtegida`. `/configuracoes` (Story 1.7, "Meu Perfil" + solicitação de
 * promoção de papel) é rota-filha da raiz, dentro do `AppShell`/`RotaProtegida`.
 * A árvore do `AppShell` fica atrás do `RotaProtegida`
 * (Story 1.5): o `AuthProvider` faz o bootstrap silencioso da sessão ao
 * montar o app (silent refresh via cookie), e enquanto isso não resolve a
 * rota protegida mostra uma tela mínima de carregamento; conta anônima é
 * redirecionada para `/login`.
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
      { index: true, element: <PlaceholderPage /> },
      { path: 'configuracoes', element: <ConfiguracoesPage /> },
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
      <RouterProvider router={router} />
    </AuthProvider>
  );
}

export default App;
