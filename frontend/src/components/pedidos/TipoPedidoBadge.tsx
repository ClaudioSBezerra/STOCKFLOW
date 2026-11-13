import { ArrowLeftRight, type LucideIcon } from 'lucide-react';
import type { TipoPedido } from '@/lib/pedidos';

/**
 * Badge de TIPO de Pedido (Story 20.1, Epic 20, spec-20-1) — rótulo próprio
 * exibido ao lado de `StatusPedidoBadge` na Fila/Meus Pedidos, só para
 * distinguir um Pedido de Transferência entre Filiais do Pedido de retirada
 * comum (AC1: "aparece com um rótulo que o distingue"). `tipo` ausente ou
 * `'retirada'` não renderiza nada — o Pedido de retirada (Epic 7) continua
 * visualmente idêntico a antes desta story, nunca ganha um badge novo.
 * `'baixa'` (Story 20.2, ainda sem nenhuma tela que o produza) já tem um
 * rótulo pronto, a custo zero, para quando essa story existir.
 */

interface EstiloTipo {
  rotulo: string;
  Icone: LucideIcon;
  classe: string;
}

function estiloDoTipo(tipo: TipoPedido): EstiloTipo | null {
  switch (tipo) {
    case 'transferencia':
      // Tinta `info` (azul) — mesma paleta fechada de StatusPedidoBadge
      // (DESIGN.md/Epic 19): não existe uma tinta "accent" própria no
      // design system, e `info` nunca aparece sozinha nesta linha (o status
      // sempre acompanha, com ícone/texto distintos) — reaproveitar é
      // seguro, nunca ambíguo.
      return {
        rotulo: 'Transferência',
        Icone: ArrowLeftRight,
        classe: 'bg-info/10 text-[color:var(--color-text-on-tint-info)]',
      };
    case 'baixa':
      return {
        rotulo: 'Baixa',
        Icone: ArrowLeftRight,
        classe: 'bg-info/10 text-[color:var(--color-text-on-tint-info)]',
      };
    case 'retirada':
    default:
      return null;
  }
}

export function TipoPedidoBadge({ tipo }: { tipo?: TipoPedido }) {
  const estilo = tipo ? estiloDoTipo(tipo) : null;
  if (!estilo) {
    return null;
  }
  const { rotulo, Icone, classe } = estilo;
  return (
    <span
      className={`text-label inline-flex items-center gap-1 rounded-full px-2 py-0.5 font-medium ${classe}`}
    >
      <Icone aria-hidden className="size-3.5 shrink-0" />
      {rotulo}
    </span>
  );
}

export default TipoPedidoBadge;
