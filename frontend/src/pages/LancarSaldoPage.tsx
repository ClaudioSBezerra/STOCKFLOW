import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { LancamentoSaldoSection } from '@/components/estoques/LancamentoSaldoSection';

/**
 * Página "Lançar saldo" (`/estoques/lancar-saldo`, Story 17.1 — antes a aba
 * de Estoques). Gate `almoxarife`+; o `h1` "Lançar saldo" vem da seção.
 */
export function LancarSaldoPage() {
  const { usuario } = useAuth();
  const podeGerir = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeGerir ? (
        <LancamentoSaldoSection />
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso à área de Estoques.
        </p>
      )}
    </div>
  );
}

export default LancarSaldoPage;
