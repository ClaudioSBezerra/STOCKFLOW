import { useCallback, useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { useAuth } from '@/lib/auth';
import { getAccessToken } from '@/lib/session';
import { rankPapel } from '@/components/shell/nav-items';
import { proximoPapel, rotuloPapel } from '@/lib/promocao';

/**
 * Página "Meu Perfil" (`/configuracoes`, Story 1.7, spec-1-7). Renderizada
 * dentro do `AppShell`/`RotaProtegida`. Duas seções empilhadas na mesma
 * página (simplificação deliberada do "aba dentro de Configurações" do
 * EXPERIENCE.md — o `AppShell` não tem abas horizontais nesta story):
 *
 *  - "Meu Perfil": identidade (`nome`/`email`/`papel` de `useAuth()`) + botão
 *    "Solicitar promoção" para quem tem papel abaixo de `gestor`. O estado do
 *    botão vem de `GET /api/promocoes/minha` (consulta sob demanda no mount —
 *    sem toast, sem notificação: isso fica para uma story posterior).
 *  - "Decidir promoções": só montada para `gestor`/`adm`. Lista de
 *    `GET /api/promocoes` com "Aprovar"/"Recusar" por item, chamando
 *    `POST /api/promocoes/{id}/decisao`.
 *
 * O backend é sempre a autoridade: o papel-alvo é derivado no servidor a
 * partir do papel atual do solicitante, nunca enviado pelo cliente. Falha de
 * rede/HTTP vira mensagem inline (`role="alert"`), consistente com
 * `CadastroPage`/`RedefinirSenhaPage`.
 */

interface MinhaSolicitacao {
  id: string;
  papel_alvo: string;
  status: 'pendente' | 'aprovada' | 'rejeitada';
  criado_em: string;
  decidido_em: string | null;
}

interface SolicitacaoPendente {
  id: string;
  solicitante_nome: string;
  solicitante_email: string;
  papel_atual: string;
  papel_alvo: string;
  criado_em: string;
}

function authHeaders(): Record<string, string> {
  const token = getAccessToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const MENSAGEM_ERRO_SOLICITAR =
  'Não foi possível solicitar a promoção agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_DECISAO = 'Não foi possível concluir a decisão.';
const MENSAGEM_ERRO_CARREGAR_MINHA =
  'Não foi possível verificar o estado da sua solicitação. Recarregue a página.';

export function ConfiguracoesPage() {
  const { usuario } = useAuth();
  const papel = usuario?.papel ?? '';
  const alvo = proximoPapel(papel);
  const podeDecidir = rankPapel(papel) >= rankPapel('gestor');

  const [minha, setMinha] = useState<MinhaSolicitacao | null>(null);
  const [pendentes, setPendentes] = useState<SolicitacaoPendente[]>([]);
  const [enviando, setEnviando] = useState(false);
  const [erroSolicitar, setErroSolicitar] = useState<string | null>(null);
  const [decidindoId, setDecidindoId] = useState<string | null>(null);
  const [erroDecisao, setErroDecisao] = useState<string | null>(null);
  const [avisoDecisao, setAvisoDecisao] = useState<string | null>(null);
  const [erroCarregarPendentes, setErroCarregarPendentes] = useState<string | null>(null);
  const [erroCarregarMinha, setErroCarregarMinha] = useState<string | null>(null);

  const carregarMinha = useCallback(async () => {
    try {
      const res = await fetch('/api/promocoes/minha', { headers: authHeaders() });
      if (!res.ok) {
        // Sem este alerta, uma falha de carga deixaria uma conta com
        // solicitação `pendente` vendo o botão habilitado — o clique seguinte
        // só recebe um 409 disfarçado de erro transitório. Mesmo cuidado de
        // `carregarPendentes`: nunca oferecer uma ação cuja pré-condição não
        // foi verificada.
        setErroCarregarMinha(MENSAGEM_ERRO_CARREGAR_MINHA);
        return;
      }
      const body = (await res.json()) as { solicitacao: MinhaSolicitacao | null };
      setMinha(body.solicitacao);
      setErroCarregarMinha(null);
    } catch {
      setErroCarregarMinha(MENSAGEM_ERRO_CARREGAR_MINHA);
    }
  }, []);

  const carregarPendentes = useCallback(async () => {
    try {
      const res = await fetch('/api/promocoes', { headers: authHeaders() });
      if (!res.ok) {
        // Sem este alerta, uma falha de carga deixaria o gestor/adm olhando
        // "Nenhuma solicitação pendente." — um falso "nada a fazer" que
        // esconde promoções reais aguardando decisão.
        setErroCarregarPendentes('Não foi possível carregar as solicitações pendentes.');
        return;
      }
      const body = (await res.json()) as { solicitacoes: SolicitacaoPendente[] };
      setPendentes(body.solicitacoes ?? []);
      setErroCarregarPendentes(null);
    } catch {
      setErroCarregarPendentes('Não foi possível carregar as solicitações pendentes.');
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await carregarMinha();
      if (podeDecidir) {
        await carregarPendentes();
      }
    })();
  }, [carregarMinha, carregarPendentes, podeDecidir]);

  async function solicitar() {
    // Defesa em profundidade contra duplo-submit (molde de CadastroPage): o
    // `disabled` do botão só reflete `enviando` após o próximo repaint.
    if (enviando) {
      return;
    }
    setErroSolicitar(null);
    setEnviando(true);
    try {
      const res = await fetch('/api/promocoes', { method: 'POST', headers: authHeaders() });
      if (!res.ok) {
        setErroSolicitar(MENSAGEM_ERRO_SOLICITAR);
        return;
      }
      await carregarMinha();
    } catch {
      setErroSolicitar(MENSAGEM_ERRO_SOLICITAR);
    } finally {
      setEnviando(false);
    }
  }

  async function decidir(id: string, aprovar: boolean) {
    if (decidindoId !== null) {
      return;
    }
    setErroDecisao(null);
    setAvisoDecisao(null);
    setDecidindoId(id);
    try {
      const res = await fetch(`/api/promocoes/${id}/decisao`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ aprovar }),
      });
      if (!res.ok) {
        setErroDecisao(MENSAGEM_ERRO_DECISAO);
        // Uma decisão que falha por 404/409 (o item já foi decidido por outro
        // gestor) deixaria a linha na lista para ser retentada sem fim —
        // recarrega a fila para a linha obsoleta cair.
        await carregarPendentes();
        return;
      }
      setAvisoDecisao(aprovar ? 'Promoção aprovada.' : 'Promoção recusada.');
      await carregarPendentes();
    } catch {
      setErroDecisao(MENSAGEM_ERRO_DECISAO);
      // Mesma razão do ramo `!res.ok`: uma falha aqui pode ter coincidido com
      // o item já sendo decidido por outro gestor — recarrega a fila para a
      // linha obsoleta cair em vez de ser retentada sem fim.
      await carregarPendentes();
    } finally {
      setDecidindoId(null);
    }
  }

  const pendente = minha?.status === 'pendente';
  const rejeitada = minha?.status === 'rejeitada';

  return (
    <div className="flex flex-col gap-6 p-6">
      <Card>
        <CardHeader>
          <h1 className="text-heading-lg">Meu Perfil</h1>
          <CardDescription>
            Seus dados de acesso e solicitação de promoção de papel.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <dl className="flex flex-col gap-3">
            <div className="flex flex-col">
              <dt className="text-label text-muted-foreground">Nome</dt>
              <dd className="text-body">{usuario?.nome}</dd>
            </div>
            <div className="flex flex-col">
              <dt className="text-label text-muted-foreground">E-mail</dt>
              <dd className="text-body">{usuario?.email}</dd>
            </div>
            <div className="flex flex-col">
              <dt className="text-label text-muted-foreground">Papel</dt>
              <dd className="text-body">{rotuloPapel(papel)}</dd>
            </div>
          </dl>

          <div className="flex flex-col gap-2 border-t border-border pt-4">
            <h2 className="text-heading-md">Solicitar promoção</h2>
            {alvo ? (
              <>
                <Button
                  type="button"
                  onClick={solicitar}
                  disabled={enviando || pendente || erroCarregarMinha !== null}
                  className="self-start"
                >
                  {enviando ? 'Enviando...' : `Solicitar promoção para ${rotuloPapel(alvo)}`}
                </Button>
                {pendente && (
                  <p className="text-body text-muted-foreground">
                    Solicitação pendente de aprovação.
                  </p>
                )}
                {rejeitada && (
                  <p className="text-body text-muted-foreground">
                    Sua última solicitação foi recusada.
                  </p>
                )}
                {erroCarregarMinha && (
                  <p role="alert" className="text-body text-destructive">
                    {erroCarregarMinha}
                  </p>
                )}
                {erroSolicitar && (
                  <p role="alert" className="text-body text-destructive">
                    {erroSolicitar}
                  </p>
                )}
              </>
            ) : (
              <p className="text-body text-muted-foreground">
                Não há promoção disponível para o seu papel.
              </p>
            )}
          </div>
        </CardContent>
      </Card>

      {podeDecidir && (
        <Card>
          <CardHeader>
            <h2 className="text-heading-md">Decidir promoções</h2>
            <CardDescription>Solicitações de promoção aguardando sua decisão.</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {avisoDecisao && <output className="text-body">{avisoDecisao}</output>}
            {erroDecisao && (
              <p role="alert" className="text-body text-destructive">
                {erroDecisao}
              </p>
            )}
            {erroCarregarPendentes && (
              <p role="alert" className="text-body text-destructive">
                {erroCarregarPendentes}
              </p>
            )}
            {!erroCarregarPendentes && pendentes.length === 0 && (
              <p className="text-body text-muted-foreground">Nenhuma solicitação pendente.</p>
            )}
            {!erroCarregarPendentes && pendentes.length > 0 && (
              <ul className="flex flex-col gap-3">
                {pendentes.map((p) => (
                  <li
                    key={p.id}
                    className="flex flex-col gap-2 border-b border-border pb-3 last:border-b-0 last:pb-0"
                  >
                    <div className="flex flex-col">
                      <span className="text-body">{p.solicitante_nome}</span>
                      <span className="text-label text-muted-foreground">
                        {p.solicitante_email}
                      </span>
                      <span className="text-label text-muted-foreground">
                        {rotuloPapel(p.papel_atual)} &rarr; {rotuloPapel(p.papel_alvo)}
                      </span>
                    </div>
                    <div className="flex gap-2">
                      <Button
                        type="button"
                        size="sm"
                        aria-label={`Aprovar promoção de ${p.solicitante_nome}`}
                        onClick={() => decidir(p.id, true)}
                        disabled={decidindoId !== null}
                      >
                        Aprovar
                      </Button>
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        aria-label={`Recusar promoção de ${p.solicitante_nome}`}
                        onClick={() => decidir(p.id, false)}
                        disabled={decidindoId !== null}
                      >
                        Recusar
                      </Button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}

export default ConfiguracoesPage;
