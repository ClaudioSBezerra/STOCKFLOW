import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { LocaisEstoqueSection } from '@/components/estoques/LocaisEstoqueSection';

/**
 * Página "Locais" (`/estoques`, Story 2.1; sem abas desde a Story 17.1 —
 * Lançar saldo e Movimentações são rotas próprias). Gate `almoxarife`+
 * espelhado do `nav-items.ts`; cobre a navegação direta pela URL, o servidor
 * continua sendo a autoridade. O `h1` "Locais" vem da `LocaisEstoqueSection`.
 */
export function EstoquesPage() {
  const { usuario } = useAuth();
  const podeGerir = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeGerir ? (
        <LocaisEstoqueSection />
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso à área de Estoques.
        </p>
      )}
    </div>
  );
}

export default EstoquesPage;
