import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { DecidirPromocoesSection } from '@/components/promocoes/DecidirPromocoesSection';

/**
 * Página "Promoções" (Story 17.2). Gate `gestor`+ espelhado de `nav-items.ts`;
 * sem o papel, mostra a recusa e não monta a seção (nenhum fetch).
 */
export function PromocoesPage() {
  const { usuario } = useAuth();
  const permitido = rankPapel(usuario?.papel ?? '') >= rankPapel('gestor');

  return (
    <div className="flex flex-col gap-6 p-6">
      {permitido ? (
        <>
          <h1 className="text-heading-lg">Promoções</h1>
          <DecidirPromocoesSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">Você não tem acesso a Promoções.</p>
      )}
    </div>
  );
}

export default PromocoesPage;
