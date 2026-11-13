import { useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { baixarPreNotaPDF, MENSAGEM_ERRO_PRE_NOTA } from '@/lib/pedidos';

interface BaixarPreNotaButtonProps {
  preNotaId: string;
  docnum?: number | null;
}

/**
 * Botão "Baixar Pré-Nota" (Story 20.5, spec-20-5) — usado no resultado da
 * Aprovação em lote e no Dialog de detalhe da Fila/Meus Pedidos. Só aparece
 * para `gestor`+ (gate de experiência; o servidor é a autoridade real via
 * `RequireRole(PapelGestor)` em GET /api/pre-notas/{id}/pdf). Download no
 * molde EXATO de "Baixar recibo": `Blob` -> `URL.createObjectURL` -> `<a
 * download>` criado, clicado e removido -> `URL.revokeObjectURL`, sem sair
 * da página. Falha -> `toast.error` com a mensagem do servidor.
 */
export function BaixarPreNotaButton({ preNotaId, docnum }: BaixarPreNotaButtonProps) {
  const { usuario } = useAuth();
  const [baixando, setBaixando] = useState(false);

  if (rankPapel(usuario?.papel ?? '') < rankPapel('gestor')) {
    return null;
  }

  async function aoBaixar() {
    setBaixando(true);
    try {
      const blob = await baixarPreNotaPDF(preNotaId);
      const href = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = href;
      link.download = `pre-nota-${docnum ?? preNotaId}.pdf`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      // Revogar no mesmo tick do click() pode cancelar o download no
      // Firefox/Safari — adia para depois do navegador iniciar o download.
      setTimeout(() => URL.revokeObjectURL(href), 0);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : MENSAGEM_ERRO_PRE_NOTA);
    } finally {
      setBaixando(false);
    }
  }

  return (
    <Button
      type="button"
      variant="outline"
      onClick={() => void aoBaixar()}
      disabled={baixando}
      aria-busy={baixando}
    >
      {baixando ? 'Baixando Pré-Nota...' : 'Baixar Pré-Nota'}
    </Button>
  );
}

export default BaixarPreNotaButton;
