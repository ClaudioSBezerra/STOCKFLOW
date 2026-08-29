import { createBrowserRouter, RouterProvider } from 'react-router-dom';
import { AppShell } from '@/components/shell/AppShell';
import { PlaceholderPage } from '@/pages/PlaceholderPage';
import { CadastroPage } from '@/pages/CadastroPage';
import { VerificarEmailPage } from '@/pages/VerificarEmailPage';
import { LoginPage } from '@/pages/LoginPage';

/**
 * Rota raiz usa `AppShell` como layout; todo caminho não-raiz reaproveita a
 * mesma página placeholder — nenhuma tela de produto real existe ainda
 * (ver spec-1-2). Itens de navegação do shell continuam clicáveis mesmo sem
 * uma rota "de verdade" atrás deles.
 *
 * `/cadastro`, `/verificar-email` (Story 1.3) e `/login` (Story 1.4,
 * spec-1-4) são rotas públicas irmãs da raiz, fora do `AppShell` — mesma
 * classificação de superfície pública, cada uma com seu próprio layout
 * mínimo (sem rail/bottom nav). Nenhum bootstrap automático de sessão
 * acontece aqui ao montar o app (silent refresh via cookie): `AppShell`
 * ainda não gateia nada por papel — fica para a Story 1.5.
 */
const router = createBrowserRouter([
  {
    path: '/',
    element: <AppShell />,
    children: [
      { index: true, element: <PlaceholderPage /> },
      { path: '*', element: <PlaceholderPage /> },
    ],
  },
  { path: '/cadastro', element: <CadastroPage /> },
  { path: '/verificar-email', element: <VerificarEmailPage /> },
  { path: '/login', element: <LoginPage /> },
]);

function App() {
  return <RouterProvider router={router} />;
}

export default App;
