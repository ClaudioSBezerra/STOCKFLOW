import { useCallback, useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { rotuloPapel } from '@/lib/promocao';
import {
  listarSolicitacoesExclusao,
  processarExclusaoConta,
  MENSAGEM_ERRO_LISTAR_EXCLUSAO,
  MENSAGEM_ERRO_PROCESSAR_EXCLUSAO,
  type SolicitacaoExclusao,
} from '@/lib/privacidade';

/**
 * Seção "Solicitações de exclusão" (`/configuracoes`, Story 8.2, spec-8-2).
 * `Card` montado só para `adm` (mesmo gate de "Log de Acesso"). Lista
 * `GET /api/solicitacoes-exclusao` no mount; por linha (nome/e-mail/papel/data)
 * um botão "Processar exclusão" (`variant="destructive"`) abre um
 * `ConfirmDialog` (`confirmVariant="destructive"`, `confirmLabel="Anonimizar"`)
 * antes de `POST /api/solicitacoes-exclusao/{id}/processamento`.
 *
 * A anonimização é irreversível: reescreve nome/e-mail e zera as credenciais
 * da conta alvo; o histórico de Movimentações/Pedidos/log de acesso é
 * preservado sem identificação. Falha de carga e falha de ação (inclui o
 * 409 do guard do último administrador, cuja mensagem vem do servidor) viram
 * `<p role="alert">` inline. Toda ação — sucesso OU falha — refaz a lista.
 */
export function SolicitacoesExclusaoSection() {
  const { usuario } = useAuth();
  const podeProcessar = rankPapel(usuario?.papel ?? '') >= rankPapel('adm');

  const [solicitacoes, setSolicitacoes] = useState<SolicitacaoExclusao[]>([]);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [erroAcao, setErroAcao] = useState<string | null>(null);
  const [acaoEmCurso, setAcaoEmCurso] = useState(false);
  const [alvo, setAlvo] = useState<SolicitacaoExclusao | null>(null);

  const carregar = useCallback(async () => {
    try {
      const lista = await listarSolicitacoesExclusao();
      setSolicitacoes(lista);
      setErroCarregar(null);
    } catch (err) {
      setErroCarregar(err instanceof Error ? err.message : MENSAGEM_ERRO_LISTAR_EXCLUSAO);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      if (podeProcessar) {
        await carregar();
      }
    })();
  }, [podeProcessar, carregar]);

  async function executar(id: string) {
    if (acaoEmCurso) {
      return;
    }
    setErroAcao(null);
    setAcaoEmCurso(true);
    try {
      await processarExclusaoConta(id);
    } catch (err) {
      setErroAcao(err instanceof Error ? err.message : MENSAGEM_ERRO_PROCESSAR_EXCLUSAO);
    } finally {
      setAcaoEmCurso(false);
      // Sucesso OU falha: a lista é refeita para a linha processada cair (ou
      // uma linha obsoleta cair após um 404/409).
      await carregar();
    }
  }

  function confirmarAcao() {
    if (!alvo) {
      return;
    }
    const { id } = alvo;
    setAlvo(null);
    void executar(id);
  }

  if (!podeProcessar) {
    return null;
  }

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Solicitações de exclusão</h2>
        <CardDescription>
          Contas que pediram exclusão pela LGPD. Processar anonimiza a conta de forma irreversível.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {erroAcao && (
          <p role="alert" className="text-body text-destructive">
            {erroAcao}
          </p>
        )}
        {erroCarregar && (
          <p role="alert" className="text-body text-destructive">
            {erroCarregar}
          </p>
        )}
        {!erroCarregar && solicitacoes.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhuma solicitação de exclusão pendente.</p>
        )}
        {!erroCarregar && solicitacoes.length > 0 && (
          <ul className="flex flex-col gap-3">
            {solicitacoes.map((s) => (
              <li
                key={s.id}
                className="flex flex-col gap-2 border-b border-border pb-3 last:border-b-0 last:pb-0"
              >
                <div className="flex flex-col">
                  <span className="text-body">{s.nome}</span>
                  <span className="text-label text-muted-foreground">{s.email}</span>
                  <span className="text-label text-muted-foreground">
                    {rotuloPapel(s.papel)} &middot; {new Date(s.criadoEm).toLocaleString('pt-BR')}
                  </span>
                </div>
                <div className="flex gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="destructive"
                    aria-label={`Processar exclusão da conta de ${s.nome}`}
                    onClick={() => setAlvo(s)}
                    disabled={acaoEmCurso}
                  >
                    Processar exclusão
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </CardContent>

      <ConfirmDialog
        open={alvo !== null}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setAlvo(null);
          }
        }}
        onConfirm={confirmarAcao}
        title={`Anonimizar a conta de ${alvo?.nome ?? ''}?`}
        description="Esta ação é irreversível: o nome e o e-mail da conta são substituídos por valores anônimos, as credenciais são zeradas e as sessões encerradas. As Movimentações e Pedidos que a conta gerou continuam no histórico, sem identificação."
        confirmLabel="Anonimizar"
        confirmVariant="destructive"
      />
    </Card>
  );
}

export default SolicitacoesExclusaoSection;
