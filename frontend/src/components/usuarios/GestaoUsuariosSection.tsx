import { useCallback, useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { useAuth } from '@/lib/auth';
import { getAccessToken } from '@/lib/session';
import { rankPapel } from '@/components/shell/nav-items';
import { papelAbaixo, rotuloPapel } from '@/lib/promocao';

/**
 * Seção "Gestão de Usuários" (`/configuracoes`, Story 1.8, spec-1-8). Terceiro
 * `Card` empilhado em `ConfiguracoesPage`, montado só para `gestor`/`adm` (mesmo
 * gate de "Decidir promoções"). Lista `GET /api/usuarios` — o recorte de escopo
 * é do servidor: um `gestor` só recebe contas `usuario`/`almoxarife`, um `adm`
 * recebe todas.
 *
 * Ações por linha, exceto a do próprio ator:
 *  - "Desativar"/"Reativar" conforme `ativo` -> `POST /api/usuarios/{id}/desativacao`
 *    com `{ "ativo": false | true }`.
 *  - "Rebaixar para {papel}" quando existe papel abaixo -> `POST
 *    /api/usuarios/{id}/rebaixamento` (sem corpo; o alvo é derivado no servidor).
 *
 * "Desativar" e "Rebaixar" reduzem acesso: passam por um `ConfirmDialog` único
 * (nunca `window.confirm()`). "Reativar" é direto. Falha de carga da lista e
 * falha de ação viram mensagem inline `role="alert"` (sem toast, molde de
 * `ConfiguracoesPage`); toda ação — sucesso OU falha — refaz o
 * `GET /api/usuarios`.
 */

interface UsuarioResumo {
  id: string;
  nome: string;
  email: string;
  papel: string;
  ativo: boolean;
}

type TipoAcao = 'desativar' | 'reativar' | 'rebaixar';

interface AcaoPendente {
  id: string;
  tipo: 'desativar' | 'rebaixar';
  nome: string;
  alvoRotulo?: string;
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar a lista de contas. Recarregue a página.';
const MENSAGEM_ERRO_ACAO = 'Não foi possível concluir a ação na conta.';

export function GestaoUsuariosSection() {
  const { usuario } = useAuth();
  const atorId = usuario?.id ?? '';
  const podeGerir = rankPapel(usuario?.papel ?? '') >= rankPapel('gestor');

  const [contas, setContas] = useState<UsuarioResumo[]>([]);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [erroAcao, setErroAcao] = useState<string | null>(null);
  const [acaoEmCurso, setAcaoEmCurso] = useState(false);
  const [acaoPendente, setAcaoPendente] = useState<AcaoPendente | null>(null);

  const carregar = useCallback(async () => {
    try {
      const res = await fetch('/api/usuarios', { headers: authHeaders() });
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { usuarios: UsuarioResumo[] };
      setContas(body.usuarios ?? []);
      setErroCarregar(null);
    } catch {
      setErroCarregar(MENSAGEM_ERRO_CARREGAR);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      if (podeGerir) {
        await carregar();
      }
    })();
  }, [podeGerir, carregar]);

  async function executar(id: string, tipo: TipoAcao) {
    if (acaoEmCurso) {
      return;
    }
    setErroAcao(null);
    setAcaoEmCurso(true);
    try {
      const url =
        tipo === 'rebaixar'
          ? `/api/usuarios/${id}/rebaixamento`
          : `/api/usuarios/${id}/desativacao`;
      const init: RequestInit =
        tipo === 'rebaixar'
          ? { method: 'POST', headers: authHeaders() }
          : {
              method: 'POST',
              headers: { 'Content-Type': 'application/json', ...authHeaders() },
              body: JSON.stringify({ ativo: tipo === 'reativar' }),
            };
      const res = await fetch(url, init);
      if (!res.ok) {
        setErroAcao(MENSAGEM_ERRO_ACAO);
      }
    } catch {
      setErroAcao(MENSAGEM_ERRO_ACAO);
    } finally {
      setAcaoEmCurso(false);
      // Sucesso OU falha: a lista é refeita para a linha refletir o estado
      // real (ou a linha obsoleta cair após um 404/409).
      await carregar();
    }
  }

  function confirmarAcao() {
    if (!acaoPendente) {
      return;
    }
    const { id, tipo } = acaoPendente;
    setAcaoPendente(null);
    void executar(id, tipo);
  }

  if (!podeGerir) {
    return null;
  }

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Gestão de Usuários</h2>
        <CardDescription>
          Desative, reative ou rebaixe contas de papel abaixo do seu.
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
        {!erroCarregar && contas.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhuma conta para gerir.</p>
        )}
        {!erroCarregar && contas.length > 0 && (
          <ul className="flex flex-col gap-3">
            {contas.map((c) => {
              const abaixo = papelAbaixo(c.papel);
              const ehAtor = c.id === atorId;
              return (
                <li
                  key={c.id}
                  className="flex flex-col gap-2 border-b border-border pb-3 last:border-b-0 last:pb-0"
                >
                  <div className="flex flex-col">
                    <span className="text-body">{c.nome}</span>
                    <span className="text-label text-muted-foreground">{c.email}</span>
                    <span className="text-label text-muted-foreground">
                      {rotuloPapel(c.papel)}
                      {!c.ativo && ' — inativa'}
                    </span>
                  </div>
                  {!ehAtor && (
                    <div className="flex gap-2">
                      {c.ativo ? (
                        <Button
                          type="button"
                          size="sm"
                          variant="destructive"
                          aria-label={`Desativar conta de ${c.nome}`}
                          onClick={() =>
                            setAcaoPendente({ id: c.id, tipo: 'desativar', nome: c.nome })
                          }
                          disabled={acaoEmCurso}
                        >
                          Desativar
                        </Button>
                      ) : (
                        <Button
                          type="button"
                          size="sm"
                          aria-label={`Reativar conta de ${c.nome}`}
                          onClick={() => void executar(c.id, 'reativar')}
                          disabled={acaoEmCurso}
                        >
                          Reativar
                        </Button>
                      )}
                      {abaixo && (
                        <Button
                          type="button"
                          size="sm"
                          variant="outline"
                          aria-label={`Rebaixar ${c.nome} para ${rotuloPapel(abaixo)}`}
                          onClick={() =>
                            setAcaoPendente({
                              id: c.id,
                              tipo: 'rebaixar',
                              nome: c.nome,
                              alvoRotulo: rotuloPapel(abaixo),
                            })
                          }
                          disabled={acaoEmCurso}
                        >
                          Rebaixar para {rotuloPapel(abaixo)}
                        </Button>
                      )}
                    </div>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </CardContent>

      <ConfirmDialog
        open={acaoPendente !== null}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setAcaoPendente(null);
          }
        }}
        onConfirm={confirmarAcao}
        title={
          acaoPendente?.tipo === 'rebaixar'
            ? `Rebaixar ${acaoPendente.nome} para ${acaoPendente.alvoRotulo}?`
            : `Desativar a conta de ${acaoPendente?.nome ?? ''}?`
        }
        description={
          acaoPendente?.tipo === 'rebaixar'
            ? 'A conta continua entrando, mas com menos privilégio já na próxima requisição.'
            : 'A conta perde o acesso imediatamente e as sessões ativas são encerradas.'
        }
        confirmLabel={acaoPendente?.tipo === 'rebaixar' ? 'Rebaixar' : 'Desativar'}
      />
    </Card>
  );
}

export default GestaoUsuariosSection;
