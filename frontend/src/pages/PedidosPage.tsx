import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { MeusPedidosSection } from '@/components/pedidos/MeusPedidosSection';
import { FilaPedidosSection } from '@/components/pedidos/FilaPedidosSection';

/**
 * Página "Pedidos" (`/pedidos`, Story 7.3, spec-7-3; abas da Story 7.4,
 * spec-7-4) — substitui a antiga `MeusPedidosPage` como componente da rota.
 * Renderizada dentro do `AppShell`/`RotaProtegida`, sem gate de papel
 * próprio (`usuario`+) na própria página — a aba "Meus Pedidos" existe para
 * QUALQUER papel.
 *
 * `Tabs` (`@/components/ui/tabs`, molde de `EstoquesPage`/`CatalogoPage`)
 * sempre com a aba "Meus Pedidos" (`MeusPedidosSection`, `defaultValue`),
 * mais a aba "Fila" (`FilaPedidosSection`) só quando `podeVerFila`
 * (`rankPapel(papel) >= rankPapel('almoxarife')`) — DIFERENTE de
 * `EstoquesPage`/`NormalizacaoPage` (que gateiam a árvore de `Tabs` inteira
 * atrás de um papel mínimo): aqui a aba "Meus Pedidos" nunca é gateada,
 * então o próprio `TabsTrigger`/`TabsContent` da aba "Fila" é que entra ou
 * sai condicionalmente dentro de uma `Tabs` sempre presente.
 *
 * Gate de papel espelhado do `nav-items.ts`/`EstoquesPage`: o servidor
 * continua sendo a autoridade real — um Usuário sem papel `almoxarife`+ que
 * force `GET /api/pedidos?escopo=todos` recebe só os próprios Pedidos
 * (`services.ListarPedidosParaSessao`), nunca 403, nunca os Pedidos alheios
 * (epics.md Story 7.4 AC2); este espelho é só de experiência — a aba "Fila"
 * não aparece na tela nem é alcançável por clique para quem não tem o
 * papel.
 *
 * `key={String(podeVerFila)}` em `Tabs` força a remontagem inteira da árvore
 * de abas sempre que `podeVerFila` muda de valor (ex.: uma rebaixada de
 * papel refletida no `AuthProvider` em tempo real, sem reload de página,
 * enquanto "Fila" era a aba ativa) — sem isso, `Tabs` é não-controlado
 * (`defaultValue`) e o estado interno do Radix continua apontando para
 * `"fila"` mesmo depois do `TabsTrigger`/`TabsContent` daquela aba sumirem
 * do JSX, deixando a tela sem nenhum painel visível até um clique manual em
 * "Meus Pedidos". A remontagem sempre volta para `defaultValue="meus"`,
 * nunca uma aba inexistente.
 */
export function PedidosPage() {
  const { usuario } = useAuth();
  const podeVerFila = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      <Tabs key={String(podeVerFila)} defaultValue="meus">
        <TabsList>
          <TabsTrigger value="meus">Meus Pedidos</TabsTrigger>
          {podeVerFila && <TabsTrigger value="fila">Fila</TabsTrigger>}
        </TabsList>
        <TabsContent value="meus">
          <MeusPedidosSection />
        </TabsContent>
        {podeVerFila && (
          <TabsContent value="fila">
            <FilaPedidosSection />
          </TabsContent>
        )}
      </Tabs>
    </div>
  );
}

export default PedidosPage;
