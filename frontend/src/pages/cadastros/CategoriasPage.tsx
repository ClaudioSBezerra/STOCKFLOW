import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { CategoriasSection } from '@/components/categorias/CategoriasSection';

/**
 * Página "Categorias" (Story 17.2). Gate `adm`+ espelhado de `nav-items.ts`;
 * sem o papel, mostra a recusa e não monta a seção (nenhum fetch).
 */
export function CategoriasPage() {
  const { usuario } = useAuth();
  const permitido = rankPapel(usuario?.papel ?? '') >= rankPapel('adm');

  return (
    <div className="flex flex-col gap-6 p-6">
      {permitido ? (
        <>
          <h1 className="text-heading-lg">Categorias</h1>
          <CategoriasSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">Você não tem acesso a Categorias.</p>
      )}
    </div>
  );
}

export default CategoriasPage;
