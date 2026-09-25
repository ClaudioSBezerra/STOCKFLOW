import { MeusPedidosSection } from '@/components/pedidos/MeusPedidosSection';

/**
 * Página "Meus pedidos" (`/pedidos`, Story 7.3; sem abas desde a Story 17.1).
 * Sem gate de papel próprio (`usuario`+): consulta dos Pedidos próprios. A
 * Fila de aprovação virou rota própria (`/pedidos/fila`, `PedidosFilaPage`).
 * O `h1` "Meus Pedidos" vem da própria `MeusPedidosSection`.
 */
export function PedidosPage() {
  return (
    <div className="flex flex-col gap-6 p-6">
      <MeusPedidosSection />
    </div>
  );
}

export default PedidosPage;
