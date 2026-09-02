import { useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { InconsistenciasSection } from '@/components/normalizacao/InconsistenciasSection';
import { DuplicatasSection } from '@/components/normalizacao/DuplicatasSection';

/**
 * Página "Normalização" (`/normalizacao`, Story 6.1, spec-6-1; abas da Story
 * 6.3, spec-6-3). Renderizada dentro do `AppShell`/`RotaProtegida`. Duas
 * abas (`@/components/ui/tabs`, molde de `EstoquesPage`, Story 5.3):
 * "Inconsistências" (`InconsistenciasSection`, Story 6.1/6.2) e "Duplicatas"
 * (`DuplicatasSection`, Story 6.3).
 *
 * `?verificarDuplicatas=1` (Story 3.4 -> 6.3: CTA "Verificar duplicatas
 * agora" do relatório de importação, `ImportacaoProdutosSection.tsx`) define
 * a aba inicial como Duplicatas E passa `autoAnalisar` para
 * `DuplicatasSection`, para que a análise já esteja em andamento sem exigir
 * um segundo clique do Almoxarife (Intent Contract, spec-6-3).
 *
 * `autoAnalisar` dispara no MÁXIMO uma vez POR VISITA a esta página — nunca
 * de novo, mesmo que o Almoxarife troque para a aba Inconsistências e volte
 * para Duplicatas depois (o que remonta `DuplicatasSection`, já que
 * `TabsContent`/Radix desmonta o conteúdo da aba inativa por padrão; um guard
 * `useRef` local DENTRO de `DuplicatasSection` resetaria a cada remontagem e
 * dispararia a análise de novo — fix do code review de spec-6-3). O guard
 * "já disparei nesta visita" mora AQUI, em `autoAnalisarConsumido`, porque
 * `NormalizacaoPage` não desmonta ao trocar de aba (só `DuplicatasSection`
 * desmonta/remonta) — `DuplicatasSection` chama `onAutoAnalisado` no exato
 * instante em que dispara a análise automática, o que marca o guard como
 * consumido e faz `autoAnalisar` recalcular para `false` a partir daí.
 *
 * Gate de papel espelhado do `nav-items.ts` (o item de nav "Normalização" já
 * tem `papelMinimo: 'almoxarife'`): `rankPapel(papel) >=
 * rankPapel('almoxarife')`. O item de nav já não aparece para papéis abaixo;
 * este gate cobre a navegação direta pela URL. O servidor continua sendo a
 * autoridade real — GET /api/normalizacao/inconsistencias e GET
 * /api/normalizacao/duplicatas respondem 403 para papéis abaixo de
 * `almoxarife` mesmo em chamada direta à API; este espelho é só de
 * experiência.
 */
export function NormalizacaoPage() {
  const { usuario } = useAuth();
  const podeGerir = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');
  const [searchParams] = useSearchParams();
  const abaInicial = searchParams.get('verificarDuplicatas') === '1' ? 'duplicatas' : 'inconsistencias';
  const [autoAnalisarConsumido, setAutoAnalisarConsumido] = useState(false);
  const autoAnalisar = abaInicial === 'duplicatas' && !autoAnalisarConsumido;

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeGerir ? (
        <Tabs defaultValue={abaInicial}>
          <TabsList>
            <TabsTrigger value="inconsistencias">Inconsistências</TabsTrigger>
            <TabsTrigger value="duplicatas">Duplicatas</TabsTrigger>
          </TabsList>
          <TabsContent value="inconsistencias">
            <InconsistenciasSection />
          </TabsContent>
          <TabsContent value="duplicatas">
            <DuplicatasSection
              autoAnalisar={autoAnalisar}
              onAutoAnalisado={() => setAutoAnalisarConsumido(true)}
            />
          </TabsContent>
        </Tabs>
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso à área de Normalização.
        </p>
      )}
    </div>
  );
}

export default NormalizacaoPage;
