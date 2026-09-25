import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { MovimentacoesSection } from '@/components/estoques/MovimentacoesSection';

/**
 * Página "Movimentações" (`/estoques/movimentacoes`, Story 17.1 — antes a aba
 * de Estoques). Gate `almoxarife`+ (o servidor é a autoridade).
 */
export function MovimentacoesPage() {
  const { usuario } = useAuth();
  const podeGerir = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeGerir ? (
        <>
          <h1 className="text-heading-lg">Movimentações</h1>
          <MovimentacoesSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso à área de Estoques.
        </p>
      )}
    </div>
  );
}

export default MovimentacoesPage;
