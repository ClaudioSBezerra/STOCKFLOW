import { Navigate, useSearchParams } from 'react-router-dom';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { InconsistenciasSection } from '@/components/normalizacao/InconsistenciasSection';

/**
 * Página "Inconsistências" (`/normalizacao`, Story 6.1; sem abas desde a
 * Story 17.1 — Duplicatas é rota própria, `DuplicatasPage`).
 *
 * Link antigo: `/normalizacao?verificarDuplicatas=1` (CTA da importação, e-mails,
 * favoritos) redireciona para `/normalizacao/duplicatas?verificarDuplicatas=1`,
 * que dispara a análise uma vez.
 *
 * Gate `almoxarife`+ espelhado do `nav-items.ts`; o servidor continua sendo a
 * autoridade real (403 abaixo do papel, mesmo em chamada direta à API).
 */
export function NormalizacaoPage() {
  const { usuario } = useAuth();
  const podeGerir = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');
  const [searchParams] = useSearchParams();

  if (searchParams.get('verificarDuplicatas') === '1') {
    return <Navigate to="/normalizacao/duplicatas?verificarDuplicatas=1" replace />;
  }

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeGerir ? (
        <>
          <h1 className="text-heading-lg">Inconsistências</h1>
          <InconsistenciasSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso à área de Normalização.
        </p>
      )}
    </div>
  );
}

export default NormalizacaoPage;
