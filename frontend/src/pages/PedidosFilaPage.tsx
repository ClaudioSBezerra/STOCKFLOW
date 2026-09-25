import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { FilaPedidosSection } from '@/components/pedidos/FilaPedidosSection';

/**
 * Página "Fila de aprovação" (`/pedidos/fila`, Story 17.1 — antes a aba
 * "Fila" de Pedidos). Gate `almoxarife`+ espelhado do `nav-items.ts`; o
 * servidor continua sendo a autoridade (um Usuário abaixo só recebe os
 * próprios Pedidos). O `h1` "Fila de Pedidos" vem da `FilaPedidosSection`.
 */
export function PedidosFilaPage() {
  const { usuario } = useAuth();
  const podeVerFila = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeVerFila ? (
        <FilaPedidosSection />
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso à Fila de aprovação.
        </p>
      )}
    </div>
  );
}

export default PedidosFilaPage;
