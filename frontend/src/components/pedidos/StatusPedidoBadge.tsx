import {
  CheckCircle2,
  Clock,
  HelpCircle,
  SplitSquareHorizontal,
  XCircle,
  type LucideIcon,
} from 'lucide-react';
import type { StatusPedido } from '@/lib/pedidos';

/**
 * Badge de status de Pedido (Story 7.3, spec-7-3 — UX-DR6/UX-DR10): formato
 * pill, SEMPRE ícone + texto (nunca só cor), texto na variante
 * `text-on-tint-*` correspondente sobre um fundo tintado a 10% da mesma cor
 * (contraste WCAG AA). O `switch` é exaustivo sobre `StatusPedido` com um
 * `default` genérico — um valor inesperado do servidor nunca é rotulado
 * silenciosamente como um dos status conhecidos, cai no rótulo
 * "Desconhecido" com ícone neutro.
 */

interface EstiloStatus {
  rotulo: string;
  Icone: LucideIcon;
  /** Classe do pill: fundo tintado + cor de texto AA. */
  classe: string;
}

function estiloDoStatus(status: string): EstiloStatus {
  switch (status as StatusPedido) {
    case 'pendente':
      return {
        rotulo: 'Pendente',
        Icone: Clock,
        classe: 'bg-warning/10 text-[color:var(--color-text-on-tint-warning)]',
      };
    case 'aprovado':
      return {
        rotulo: 'Aprovado',
        Icone: CheckCircle2,
        classe: 'bg-success/10 text-[color:var(--color-text-on-tint-success)]',
      };
    case 'parcialmente_aprovado':
      // Tinta `info` (azul) — deliberadamente NÃO `accent`/`success`: no
      // token set do DESIGN.md, --color-accent É literalmente igual a
      // --color-success (#16a249); usar accent aqui seria visualmente
      // idêntico a 'aprovado'. `info` é a única tinta ainda não usada por
      // nenhum outro status de Pedido — garante "tinta/ícone distintos de
      // aprovado/pendente" (spec-7-5) sem introduzir cor nova fora do
      // DESIGN.md.
      return {
        rotulo: 'Parcialmente aprovado',
        Icone: SplitSquareHorizontal,
        classe: 'bg-info/10 text-[color:var(--color-text-on-tint-info)]',
      };
    case 'rejeitado':
      return {
        rotulo: 'Rejeitado',
        Icone: XCircle,
        classe: 'bg-destructive/10 text-[color:var(--color-text-on-tint-destructive)]',
      };
    default:
      return {
        rotulo: 'Desconhecido',
        Icone: HelpCircle,
        classe: 'bg-muted text-muted-foreground',
      };
  }
}

export function StatusPedidoBadge({ status }: { status: string }) {
  const { rotulo, Icone, classe } = estiloDoStatus(status);
  return (
    <span
      className={`text-label inline-flex items-center gap-1 rounded-full px-2 py-0.5 font-medium ${classe}`}
    >
      <Icone aria-hidden className="size-3.5 shrink-0" />
      {rotulo}
    </span>
  );
}

export default StatusPedidoBadge;
