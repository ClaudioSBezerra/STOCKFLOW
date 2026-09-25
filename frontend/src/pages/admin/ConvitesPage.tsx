import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { ConvitesSection } from '@/components/usuarios/ConvitesSection';

/**
 * Página "Convites" (Story 17.2). Gate `gestor`+ espelhado de `nav-items.ts`;
 * sem o papel, mostra a recusa e não monta a seção (nenhum fetch).
 */
export function ConvitesPage() {
  const { usuario } = useAuth();
  const permitido = rankPapel(usuario?.papel ?? '') >= rankPapel('gestor');

  return (
    <div className="flex flex-col gap-6 p-6">
      {permitido ? (
        <>
          <h1 className="text-heading-lg">Convites</h1>
          <ConvitesSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">Você não tem acesso a Convites.</p>
      )}
    </div>
  );
}

export default ConvitesPage;
