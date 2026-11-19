import { useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { baixarTermoBaixaPDF, MENSAGEM_ERRO_TERMO_BAIXA } from '@/lib/pedidos';

interface BaixarTermoBaixaButtonProps {
  termoBaixaId: string;
  numero?: number | null;
  /** Versão curta para a linha de uma lista: "Termo de Baixa nº N", tamanho `sm`. */
  compacto?: boolean;
  /** Fila de aprovação: só Gestor+ vê (quem não é Solicitante do pedido receberia 404). */
  somenteGestor?: boolean;
}

/**
 * Botão "Baixar Termo de Baixa" (spec-pedido-outras-saidas, molde de
 * BaixarPreNotaButton) — usado no resultado da
 * Aprovação em lote e no Dialog de detalhe da Fila/Meus Pedidos. Aparece
 * para quem vê o pedido (Gestor+ na Fila; o Solicitante em Meus Pedidos). O
 * servidor é a autoridade real: GET /api/termos-baixa/{id}/pdf libera Gestor+
 * e o Solicitante de pedido do Termo; outro caso -> 404. Download no
 * molde EXATO de "Baixar recibo": `Blob` -> `URL.createObjectURL` -> `<a
 * download>` criado, clicado e removido -> `URL.revokeObjectURL`, sem sair
 * da página. Falha -> `toast.error` com a mensagem do servidor.
 */
export function BaixarTermoBaixaButton({ termoBaixaId, numero, compacto = false, somenteGestor = false }: BaixarTermoBaixaButtonProps) {
  const { usuario } = useAuth();
  const [baixando, setBaixando] = useState(false);

  if (somenteGestor && rankPapel(usuario?.papel ?? '') < rankPapel('gestor')) {
    return null;
  }

  async function aoBaixar() {
    setBaixando(true);
    try {
      const blob = await baixarTermoBaixaPDF(termoBaixaId);
      const href = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = href;
      link.download = `termo-baixa-${numero ?? termoBaixaId}.pdf`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      // Revogar no mesmo tick do click() pode cancelar o download no
      // Firefox/Safari — adia para depois do navegador iniciar o download.
      setTimeout(() => URL.revokeObjectURL(href), 0);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : MENSAGEM_ERRO_TERMO_BAIXA);
    } finally {
      setBaixando(false);
    }
  }

  const rotuloCompacto = numero != null ? `Termo de Baixa nº ${numero}` : 'Termo de Baixa';

  return (
    <Button
      type="button"
      variant="outline"
      size={compacto ? 'sm' : undefined}
      onClick={() => void aoBaixar()}
      disabled={baixando}
      aria-busy={baixando}
      aria-label={compacto ? `Baixar ${rotuloCompacto}` : undefined}
    >
      {baixando ? 'Baixando Termo de Baixa...' : compacto ? rotuloCompacto : 'Baixar Termo de Baixa'}
    </Button>
  );
}

export default BaixarTermoBaixaButton;
