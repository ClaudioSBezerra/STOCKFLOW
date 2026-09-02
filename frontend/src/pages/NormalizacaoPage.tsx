import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { InconsistenciasSection } from '@/components/normalizacao/InconsistenciasSection';

/**
 * Página "Normalização" (`/normalizacao`, Story 6.1, spec-6-1). Renderizada
 * dentro do `AppShell`/`RotaProtegida`. Página única — uma seção só
 * (Inconsistências); "Duplicatas" é Epic 6.3/6.4, ainda sem `Tabs` até a
 * segunda seção existir (mesmo padrão de EstoquesPage na Story 2.1, que só
 * ganhou abas quando a segunda seção existiu, Story 5.3).
 *
 * Gate de papel espelhado do `nav-items.ts` (o item de nav "Normalização" já
 * tem `papelMinimo: 'almoxarife'`): `rankPapel(papel) >=
 * rankPapel('almoxarife')`. O item de nav já não aparece para papéis abaixo;
 * este gate cobre a navegação direta pela URL. O servidor continua sendo a
 * autoridade real — GET /api/normalizacao/inconsistencias responde 403 para
 * papéis abaixo de `almoxarife` mesmo em chamada direta à API; este espelho
 * é só de experiência.
 */
export function NormalizacaoPage() {
  const { usuario } = useAuth();
  const podeGerir = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeGerir ? (
        <InconsistenciasSection />
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso à área de Normalização.
        </p>
      )}
    </div>
  );
}

export default NormalizacaoPage;
