/**
 * PilhulaStatus — pílula suave de status (Story 17.3).
 *
 * Extraída do inline existente em `CatalogoListagem.tsx` (modo grade,
 * "Inativo"), `MovimentacoesSection.tsx`, etc. Componente de display puro:
 * recebe o texto do status e o exibe numa pílula com borda e cor muted.
 */

interface PilhulaStatusProps {
  status: string;
}

export function PilhulaStatus({ status }: PilhulaStatusProps) {
  return (
    <span className="rounded-full border border-border px-2 py-0.5 text-label text-muted-foreground">
      {status}
    </span>
  );
}

export default PilhulaStatus;
