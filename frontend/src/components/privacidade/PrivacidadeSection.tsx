import { useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import {
  baixarMeusDadosBlob,
  MENSAGEM_ERRO_EXPORTAR,
  MENSAGEM_ERRO_SOLICITAR_EXCLUSAO,
  solicitarExclusaoConta,
} from '@/lib/privacidade';

/**
 * Seção "Privacidade" (`/configuracoes`, Story 8.1/8.2, spec-8-1/spec-8-2).
 * Montada incondicionalmente para QUALQUER Usuário autenticado (sem gate de
 * `rankPapel`, ao contrário de "Gestão de Usuários"/"Log de Acesso") — a LGPD
 * exige que qualquer papel consiga baixar os próprios dados E pedir a exclusão
 * da própria conta.
 *
 *  - "Baixar meus dados" (Story 8.1): `Blob` de
 *    `GET /api/usuarios/me/exportar-dados` -> `<a download="meus-dados.json">`
 *    criado/clicado/removido. Falha -> `toast.error`.
 *  - "Solicitar exclusão de conta" (Story 8.2): passa por um `ConfirmDialog`
 *    (`variant="destructive"` no botão) e então
 *    `POST /api/usuarios/me/solicitacao-exclusao`. NÃO há exclusão imediata —
 *    a conta fica numa fila `pendente` para um `adm` revisar e anonimizar.
 *    Sucesso -> `toast.success`; 409 (já há pendente) -> `toast.error` com a
 *    mensagem do servidor. Guarda de duplo-clique local (`solicitando`).
 */
export function PrivacidadeSection() {
  const [baixando, setBaixando] = useState(false);
  const [solicitando, setSolicitando] = useState(false);
  const [confirmarExclusao, setConfirmarExclusao] = useState(false);

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

  async function aoSolicitarExclusao() {
    if (solicitando) {
      return;
    }
    setSolicitando(true);
    try {
      await solicitarExclusaoConta();
      toast.success(
        'Solicitação registrada. Um administrador vai revisar e anonimizar sua conta.',
      );
    } catch (err) {
      toast.error(err instanceof Error ? err.message : MENSAGEM_ERRO_SOLICITAR_EXCLUSAO);
    } finally {
      setSolicitando(false);
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
      <CardContent className="flex flex-col gap-4">
        <Button type="button" onClick={aoBaixarMeusDados} disabled={baixando} className="self-start">
          {baixando ? 'Baixando...' : 'Baixar meus dados'}
        </Button>

        <div className="flex flex-col gap-2 border-t border-border pt-4">
          <p className="text-body text-muted-foreground">
            A exclusão da conta é feita por um administrador: sua solicitação entra numa fila e a
            conta é anonimizada. O vínculo com seu histórico de Movimentações e Pedidos é mantido
            para preservar a integridade do registro — texto que você já tiver digitado (como o
            solicitante de um pedido) não é reescrito.
          </p>
          <Button
            type="button"
            variant="destructive"
            onClick={() => setConfirmarExclusao(true)}
            disabled={solicitando}
            className="self-start"
          >
            {solicitando ? 'Solicitando...' : 'Solicitar exclusão de conta'}
          </Button>
        </div>
      </CardContent>

      <ConfirmDialog
        open={confirmarExclusao}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setConfirmarExclusao(false);
          }
        }}
        onConfirm={() => {
          setConfirmarExclusao(false);
          void aoSolicitarExclusao();
        }}
        title="Solicitar exclusão da sua conta?"
        description="Um administrador vai revisar a solicitação e anonimizar sua conta: nome e e-mail são substituídos por valores anônimos e você perde o acesso. O vínculo com seu histórico de Movimentações e Pedidos é mantido para preservar a integridade do registro, mas texto que você já tiver digitado (como o solicitante de um pedido) não é reescrito."
        confirmLabel="Solicitar exclusão"
        confirmVariant="destructive"
      />
    </Card>
  );
}
