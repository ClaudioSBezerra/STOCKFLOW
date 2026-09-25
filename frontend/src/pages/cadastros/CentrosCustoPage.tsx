import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { CentrosCustoSection } from '@/components/centroscusto/CentrosCustoSection';

/**
 * Página "Centros de custo" (Story 17.2). Gate `adm`+ espelhado de `nav-items.ts`;
 * sem o papel, mostra a recusa e não monta a seção (nenhum fetch).
 */
export function CentrosCustoPage() {
  const { usuario } = useAuth();
  const permitido = rankPapel(usuario?.papel ?? '') >= rankPapel('adm');

  return (
    <div className="flex flex-col gap-6 p-6">
      {permitido ? (
        <>
          <h1 className="text-heading-lg">Centros de custo</h1>
          <CentrosCustoSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">Você não tem acesso a Centros de custo.</p>
      )}
    </div>
  );
}

export default CentrosCustoPage;
