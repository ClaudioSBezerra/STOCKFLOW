import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { FiliaisSection } from '@/components/filiais/FiliaisSection';

/**
 * Página "Filiais" (Story 17.2). Gate `adm`+ espelhado de `nav-items.ts`;
 * sem o papel, mostra a recusa e não monta a seção (nenhum fetch).
 */
export function FiliaisPage() {
  const { usuario } = useAuth();
  const permitido = rankPapel(usuario?.papel ?? '') >= rankPapel('adm');

  return (
    <div className="flex flex-col gap-6 p-6">
      {permitido ? (
        <>
          <h1 className="text-heading-lg">Filiais</h1>
          <FiliaisSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">Você não tem acesso a Filiais.</p>
      )}
    </div>
  );
}

export default FiliaisPage;
