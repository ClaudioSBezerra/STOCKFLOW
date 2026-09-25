import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { TemplatesNomenclaturaSection } from '@/components/nomenclatura/TemplatesNomenclaturaSection';

/**
 * Página "Templates de nome" (Story 17.2). Gate `adm`+ espelhado de `nav-items.ts`;
 * sem o papel, mostra a recusa e não monta a seção (nenhum fetch).
 */
export function TemplatesPage() {
  const { usuario } = useAuth();
  const permitido = rankPapel(usuario?.papel ?? '') >= rankPapel('adm');

  return (
    <div className="flex flex-col gap-6 p-6">
      {permitido ? (
        <>
          <h1 className="text-heading-lg">Templates de nome</h1>
          <TemplatesNomenclaturaSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">Você não tem acesso a Templates de nome.</p>
      )}
    </div>
  );
}

export default TemplatesPage;
