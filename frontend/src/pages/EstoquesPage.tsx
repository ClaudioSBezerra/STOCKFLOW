import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { LocaisEstoqueSection } from '@/components/estoques/LocaisEstoqueSection';

/**
 * Página "Estoques" (`/estoques`, Story 2.1, spec-2-1). Renderizada dentro do
 * `AppShell`/`RotaProtegida`. Página única — a tela "Locais": não há
 * sub-navegação de abas (Movimentações é o Epic 5).
 *
 * Gate de papel espelhado do `nav-items.ts` (o item de nav "Estoques" já tem
 * `papelMinimo: 'almoxarife'`): `rankPapel(papel) >= rankPapel('almoxarife')`.
 * O item de nav já não aparece para papéis abaixo; este gate cobre a
 * navegação direta pela URL. O servidor continua sendo a autoridade real —
 * `POST /api/estoques` responde 403 para papéis abaixo de `almoxarife` mesmo
 * em chamada direta à API; este espelho é só de experiência.
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
