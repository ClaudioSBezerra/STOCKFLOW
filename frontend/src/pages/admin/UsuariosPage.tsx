import { useState } from 'react';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { GestaoUsuariosSection } from '@/components/usuarios/GestaoUsuariosSection';
import { CriarUsuarioSection } from '@/components/usuarios/CriarUsuarioSection';

/**
 * Página "Usuários" (Story 17.2). Gate `gestor`+ espelhado de `nav-items.ts`;
 * sem o papel, mostra a recusa e não monta a seção (nenhum fetch).
 *
 * `CriarUsuarioSection` (feedback Ferreira Costa, direção, 2026-10-21) só se
 * automonta para `adm` — gate próprio, dentro do componente. `refreshKey`
 * força `GestaoUsuariosSection` a remontar (e refazer `GET /api/usuarios`)
 * depois de uma criação bem-sucedida, sem acoplar os dois componentes.
 */
export function UsuariosPage() {
  const { usuario } = useAuth();
  const permitido = rankPapel(usuario?.papel ?? '') >= rankPapel('gestor');
  const [refreshKey, setRefreshKey] = useState(0);

  return (
    <div className="flex flex-col gap-6 p-6">
      {permitido ? (
        <>
          <h1 className="text-heading-lg">Usuários</h1>
          <CriarUsuarioSection onCriada={() => setRefreshKey((k) => k + 1)} />
          <GestaoUsuariosSection key={refreshKey} />
        </>
      ) : (
        <p className="text-body text-muted-foreground">Você não tem acesso a Usuários.</p>
      )}
    </div>
  );
}

export default UsuariosPage;
