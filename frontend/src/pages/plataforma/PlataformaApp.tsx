import { useEffect, useState, type ReactNode } from 'react';
import { createBrowserRouter, Navigate, RouterProvider } from 'react-router-dom';
import { getTokenPlataforma, renovarSessaoPlataforma } from '@/lib/plataforma';
import { PlataformaLoginPage } from './PlataformaLoginPage';
import { EmpresasPage } from './EmpresasPage';

type EstadoSessaoPlataforma = 'carregando' | 'autenticado' | 'anonimo';

/**
 * Guarda da área do Dono: com o token já em memória (acabou de logar),
 * libera na hora; senão tenta restaurar a sessão pelo cookie de refresh da
 * Plataforma ao montar. Sem sessão -> `/plataforma/login`.
 */
export function GuardaPlataforma({ children }: { children: ReactNode }) {
  const [estado, setEstado] = useState<EstadoSessaoPlataforma>(() =>
    getTokenPlataforma() ? 'autenticado' : 'carregando',
  );

  useEffect(() => {
    if (estado !== 'carregando') {
      return;
    }
    let ativo = true;
    void renovarSessaoPlataforma().then((ok) => {
      if (ativo) {
        setEstado(ok ? 'autenticado' : 'anonimo');
      }
    });
    return () => {
      ativo = false;
    };
  }, [estado]);

  if (estado === 'carregando') {
    return (
      <output className="flex min-h-svh items-center justify-center text-muted-foreground">
        Carregando...
      </output>
    );
  }
  if (estado === 'autenticado') {
    return <>{children}</>;
  }
  return <Navigate to="/plataforma/login" replace />;
}

/**
 * App da Plataforma — Story 9.2 (spec-9-2). Router PRÓPRIO, montado por
 * `main.tsx` só em `/plataforma/...`: nunca carrega `AuthProvider`/
 * `CarrinhoProvider` nem a sessão de `usuarios`, e fica fora da navegação de
 * qualquer Empresa.
 */
const router = createBrowserRouter([
  { path: '/plataforma/login', element: <PlataformaLoginPage /> },
  {
    path: '/plataforma',
    element: (
      <GuardaPlataforma>
        <EmpresasPage />
      </GuardaPlataforma>
    ),
  },
  { path: '*', element: <Navigate to="/plataforma" replace /> },
]);

export function PlataformaApp() {
  return <RouterProvider router={router} />;
}

export default PlataformaApp;
