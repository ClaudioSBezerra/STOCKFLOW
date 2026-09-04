import { useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { baixarMeusDadosBlob, MENSAGEM_ERRO_EXPORTAR } from '@/lib/privacidade';

/**
 * Seção "Privacidade" (`/configuracoes`, Story 8.1, spec-8-1). Montada
 * incondicionalmente para QUALQUER Usuário autenticado (sem gate de
 * `rankPapel`, ao contrário de "Gestão de Usuários"/"Log de Acesso") — a
 * LGPD exige que qualquer papel consiga baixar os próprios dados.
 *
 * Um único botão "Baixar meus dados": `Blob` de
 * `GET /api/usuarios/me/exportar-dados` -> `URL.createObjectURL` -> um
 * `<a download="meus-dados.json">` criado, clicado e removido ->
 * `URL.revokeObjectURL` — mesmo molde de `aoBaixarRecibo`
 * (`components/pedidos/MeusPedidosSection.tsx`, Story 7.6). Falha de
 * rede/servidor -> `toast.error`, nenhum download é iniciado. Guarda de
 * duplo-clique local (`baixando`), mesmo padrão de `baixandoRecibo`.
 */
export function PrivacidadeSection() {
  const [baixando, setBaixando] = useState(false);

  async function aoBaixarMeusDados() {
    setBaixando(true);
    try {
      const blob = await baixarMeusDadosBlob();
      const href = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = href;
      link.download = 'meus-dados.json';
      document.body.appendChild(link);
      link.click();
      link.remove();
      URL.revokeObjectURL(href);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : MENSAGEM_ERRO_EXPORTAR);
    } finally {
      setBaixando(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Privacidade</h2>
        <CardDescription>
          Baixe uma cópia dos seus dados pessoais: identidade, log de acesso, Movimentações e
          Pedidos que você mesmo gerou.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Button type="button" onClick={aoBaixarMeusDados} disabled={baixando} className="self-start">
          {baixando ? 'Baixando...' : 'Baixar meus dados'}
        </Button>
      </CardContent>
    </Card>
  );
}
