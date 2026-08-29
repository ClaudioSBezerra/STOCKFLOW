import { createBrowserRouter, RouterProvider } from 'react-router-dom';
import { AppShell } from '@/components/shell/AppShell';
import { PlaceholderPage } from '@/pages/PlaceholderPage';

/**
 * Rota raiz usa `AppShell` como layout; todo caminho não-raiz reaproveita a
 * mesma página placeholder — nenhuma tela de produto real existe ainda
 * (ver spec-1-2). Itens de navegação do shell continuam clicáveis mesmo sem
 * uma rota "de verdade" atrás deles.
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
]);

function App() {
  return <RouterProvider router={router} />;
}

export default App;
