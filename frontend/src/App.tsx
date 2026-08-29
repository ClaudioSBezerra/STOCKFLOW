import { createBrowserRouter, RouterProvider } from 'react-router-dom';
import { AppShell } from '@/components/shell/AppShell';
import { PlaceholderPage } from '@/pages/PlaceholderPage';
import { CadastroPage } from '@/pages/CadastroPage';
import { VerificarEmailPage } from '@/pages/VerificarEmailPage';

/**
 * Rota raiz usa `AppShell` como layout; todo caminho não-raiz reaproveita a
 * mesma página placeholder — nenhuma tela de produto real existe ainda
 * (ver spec-1-2). Itens de navegação do shell continuam clicáveis mesmo sem
 * uma rota "de verdade" atrás deles.
 *
 * `/cadastro` e `/verificar-email` (Story 1.3, spec-1-3) são rotas públicas
 * irmãs da raiz, fora do `AppShell` — mesma classificação de superfície
 * pública do Login (Story 1.4, ainda não implementado), cada uma com seu
 * próprio layout mínimo (sem rail/bottom nav).
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
]);

function App() {
  return <RouterProvider router={router} />;
}

export default App;
