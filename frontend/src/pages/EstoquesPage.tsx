import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { LocaisEstoqueSection } from '@/components/estoques/LocaisEstoqueSection';
import { MovimentacoesSection } from '@/components/estoques/MovimentacoesSection';

/**
 * Página "Estoques" (`/estoques`, Story 2.1, spec-2-1; abas da Story 5.3,
 * spec-5-3). Renderizada dentro do `AppShell`/`RotaProtegida`. Duas abas
 * (`@/components/ui/tabs`, molde de `CatalogoPage`): "Locais"
 * (`LocaisEstoqueSection`, cadastro/exclusão de Estoques) e "Movimentações"
 * (`MovimentacoesSection`, trilha só-leitura de Baixas/Transferências que
 * assina o canal SSE `movimentacoes`).
 *
 * Gate de papel espelhado do `nav-items.ts` (o item de nav "Estoques" já tem
 * `papelMinimo: 'almoxarife'`): `rankPapel(papel) >= rankPapel('almoxarife')`.
 * O item de nav já não aparece para papéis abaixo; este gate cobre a
 * navegação direta pela URL — e envolve as DUAS abas (Movimentações também é
 * `almoxarife`+, decisão do servidor em `GET /api/movimentacoes`). O servidor
 * continua sendo a autoridade real; este espelho é só de experiência.
 */
export function EstoquesPage() {
  const { usuario } = useAuth();
  const podeGerir = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeGerir ? (
        <Tabs defaultValue="locais">
          <TabsList>
            <TabsTrigger value="locais">Locais</TabsTrigger>
            <TabsTrigger value="movimentacoes">Movimentações</TabsTrigger>
          </TabsList>
          <TabsContent value="locais">
            <LocaisEstoqueSection />
          </TabsContent>
          <TabsContent value="movimentacoes">
            <MovimentacoesSection />
          </TabsContent>
        </Tabs>
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso à área de Estoques.
        </p>
      )}
    </div>
  );
}

export default EstoquesPage;
