import { useCallback, useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { rankPapel } from '@/components/shell/nav-items';
import { useAuth } from '@/lib/auth';
import {
  alterarExigenciaMFA,
  listarAuditoriaSeguranca,
  MENSAGEM_ERRO_LISTAR_AUDITORIA,
  MENSAGEM_ERRO_OBTER_EXIGENCIA,
  obterExigenciaMFA,
  type EventoAuditoriaSeguranca,
} from '@/lib/segurancaEmpresa';

/**
 * Seção "Dupla autenticação da Empresa" (`/configuracoes`, Story 14.3,
 * FR-53/AD-35). Só o `adm` a vê (gate aqui e em `ConfiguracoesPage`); o
 * servidor continua sendo a autoridade (`RequireRole(adm)`).
 *
 *  - Mostra se a Empresa exige ("Exigida"/"Não exigida") e o botão para
 *    alternar.
 *  - "Passar a exigir" abre um `ConfirmDialog` com quantas contas
 *    `gestor`/`adm` ainda sem MFA ficarão sem acesso — nada é enviado antes de
 *    "Confirmar". "Deixar de exigir" aplica direto (não bloqueia ninguém).
 *  - No sucesso: toast, `atualizarUsuario` com `empresa.mfaObrigatorio` (o
 *    bloqueio de navegação de `App.tsx` passa a valer sem novo login) e
 *    recarga do histórico.
 *  - Histórico somente-leitura de `auditoria_seguranca` (append-only).
 */

function formatarExigencia(valor: unknown): string {
  return valor === true ? 'Exigida' : 'Não exigida';
}

function descreverEvento(e: EventoAuditoriaSeguranca): string {
  switch (e.acao) {
    case 'exigencia_alterada':
      return `${formatarExigencia(e.detalhe?.anterior)} → ${formatarExigencia(e.detalhe?.novo)}`;
    case 'mfa_resetado':
      return `MFA resetado${e.alvoNome ? ` de ${e.alvoNome}` : ''}`;
    case 'mfa_desligado':
      return `MFA desligado${e.alvoNome ? ` de ${e.alvoNome}` : ''}`;
    default:
      return e.acao;
  }
}

export function MfaEmpresaSection() {
  const { usuario, atualizarUsuario } = useAuth();
  const podeVer = rankPapel(usuario?.papel ?? '') >= rankPapel('adm');

  const [mfaObrigatorio, setMfaObrigatorio] = useState<boolean | null>(null);
  const [contasSemMfa, setContasSemMfa] = useState(0);
  const [erroExigencia, setErroExigencia] = useState<string | null>(null);
  const [eventos, setEventos] = useState<EventoAuditoriaSeguranca[]>([]);
  const [erroHistorico, setErroHistorico] = useState<string | null>(null);
  const [confirmando, setConfirmando] = useState(false);
  const [salvando, setSalvando] = useState(false);

  const carregarHistorico = useCallback(async () => {
    try {
      setEventos(await listarAuditoriaSeguranca());
      setErroHistorico(null);
    } catch {
      setErroHistorico(MENSAGEM_ERRO_LISTAR_AUDITORIA);
    }
  }, []);

  const carregarExigencia = useCallback(async () => {
    try {
      const r = await obterExigenciaMFA();
      setMfaObrigatorio(r.mfaObrigatorio);
      setContasSemMfa(r.contasSemMfa);
      setErroExigencia(null);
    } catch {
      setErroExigencia(MENSAGEM_ERRO_OBTER_EXIGENCIA);
    }
  }, []);

  useEffect(() => {
    if (!podeVer) return;
    void carregarExigencia();
    void carregarHistorico();
  }, [podeVer, carregarExigencia, carregarHistorico]);

  const aplicar = useCallback(
    async (valor: boolean) => {
      setSalvando(true);
      try {
        const r = await alterarExigenciaMFA(valor);
        setMfaObrigatorio(r.mfaObrigatorio);
        toast.success(
          r.mfaObrigatorio
            ? 'A Empresa passou a exigir dupla autenticação.'
            : 'A Empresa deixou de exigir dupla autenticação.',
        );
        if (usuario) {
          atualizarUsuario({
            ...usuario,
            empresa: { ...usuario.empresa, mfaObrigatorio: r.mfaObrigatorio },
          });
        }
        // A contagem de contas sem MFA pode ter mudado desde o mount; recarrega
        // para a próxima confirmação não mostrar um número velho.
        void carregarExigencia();
        void carregarHistorico();
      } catch (err) {
        toast.error(err instanceof Error ? err.message : 'Não foi possível alterar a exigência.');
      } finally {
        setSalvando(false);
      }
    },
    [usuario, atualizarUsuario, carregarExigencia, carregarHistorico],
  );

  if (!podeVer) {
    return null;
  }

  const descricaoConfirmacao =
    `${contasSemMfa} conta(s) gestor/adm ainda sem dupla autenticação ficarão sem acesso até cadastrar.` +
    // Só sessão por senha é bloqueada pelo gate (Story 14.1); sessão SSO nunca.
    (usuario && usuario.origem === 'senha' && !usuario.mfaHabilitado ? ' Isso inclui você.' : '');

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Dupla autenticação da Empresa</h2>
        <CardDescription>
          Define se contas gestor e adm precisam configurar a dupla autenticação para usar o
          sistema.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {erroExigencia && (
          <p role="alert" className="text-body text-destructive">
            {erroExigencia}
          </p>
        )}

        {mfaObrigatorio !== null && (
          <div className="flex flex-wrap items-center gap-3">
            <p className="text-body">
              Situação atual: <strong>{mfaObrigatorio ? 'Exigida' : 'Não exigida'}</strong>
            </p>
            <Button
              type="button"
              variant={mfaObrigatorio ? 'outline' : 'default'}
              disabled={salvando}
              onClick={() => {
                if (mfaObrigatorio) {
                  void aplicar(false);
                } else {
                  setConfirmando(true);
                }
              }}
            >
              {mfaObrigatorio ? 'Deixar de exigir' : 'Passar a exigir'}
            </Button>
          </div>
        )}

        <div className="flex flex-col gap-2">
          <h3 className="text-label text-muted-foreground">Histórico</h3>
          {erroHistorico ? (
            <p role="alert" className="text-body text-destructive">
              {erroHistorico}
            </p>
          ) : eventos.length === 0 ? (
            <p className="text-body text-muted-foreground">Nenhuma alteração registrada.</p>
          ) : (
            <ul className="flex flex-col gap-1" aria-label="Histórico de segurança">
              {eventos.map((e) => (
                <li key={e.id} className="flex flex-wrap gap-x-3 border-t border-border py-2 text-body">
                  <span className="text-muted-foreground">
                    {new Date(e.criadoEm).toLocaleString('pt-BR')}
                  </span>
                  <span>{e.atorNome ?? '—'}</span>
                  <span>{descreverEvento(e)}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </CardContent>

      <ConfirmDialog
        open={confirmando}
        onOpenChange={setConfirmando}
        title="Passar a exigir dupla autenticação?"
        description={descricaoConfirmacao}
        confirmLabel="Confirmar"
        onConfirm={() => void aplicar(true)}
      />
    </Card>
  );
}

export default MfaEmpresaSection;
