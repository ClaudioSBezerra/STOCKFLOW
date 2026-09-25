import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { QRCodeSVG } from 'qrcode.react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { mfaSetupPendente, useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { proximoPapel, rotuloPapel } from '@/lib/promocao';
import { PrivacidadeSection } from '@/components/privacidade/PrivacidadeSection';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Página "Meu perfil" (`/configuracoes`, Story 1.7; reduzida na Story 17.2).
 * Fica só com o que é da própria pessoa:
 *
 *  - Dados da conta (`nome`/`email`/`papel`) + "Solicitar promoção" para quem
 *    tem papel abaixo de `gestor` (`GET /api/promocoes/minha`,
 *    `POST /api/promocoes`).
 *  - "Segurança" (`SegurancaCard`, Stories 1.11/14.1/14.4): minha dupla
 *    autenticação (TOTP) — configurar, e desligar quando a Empresa não exige.
 *  - "Privacidade" (`PrivacidadeSection`, Story 8.1): qualquer papel baixa os
 *    próprios dados (LGPD).
 *
 * As seções administrativas (Decidir promoções, Usuários, Convites, MFA da
 * Empresa, Log de acesso, Filiais, Centros de custo, Categorias, Templates e
 * Solicitações LGPD) ganharam rota própria em `/cadastros/*` e `/admin/*`
 * (Story 17.2). O servidor é sempre a autoridade; falha de rede/HTTP vira
 * mensagem inline (`role="alert"`).
 */

interface MinhaSolicitacao {
  id: string;
  papel_alvo: string;
  status: 'pendente' | 'aprovada' | 'rejeitada';
  criado_em: string;
  decidido_em: string | null;
}

const MENSAGEM_ERRO_SOLICITAR =
  'Não foi possível solicitar a promoção agora. Tente novamente em instantes.';
const MENSAGEM_MFA_EXIGIDO_PELA_EMPRESA =
  'A Empresa exige dupla autenticação para o seu papel; ela não pode ser desligada.';
const MENSAGEM_ERRO_CARREGAR_MINHA =
  'Não foi possível verificar o estado da sua solicitação. Recarregue a página.';

/**
 * Seção "Segurança" (Story 1.11, spec-1-11): configuração de MFA (TOTP).
 * Três estados de mensagem, todos derivados de `usuario` (nunca reconsultado
 * aqui — o backend já é a autoridade em `/me`/login):
 *   - "ativo": `mfaHabilitado === true`.
 *   - "obrigatório para o seu papel": `mfaSetupPendente(usuario)` (lib/auth)
 *     — `origem==='senha' && rank>=gestor && !mfaHabilitado &&
 *     empresa.mfaObrigatorio` (Story 14.1), o MESMO helper do gate de
 *     navegação em App.tsx e espelho do 403 MFA_SETUP_REQUIRED no servidor.
 *   - "opcional": qualquer outro caso sem MFA (papel abaixo de gestor, sessão
 *     SSO, ou Empresa que não exige MFA — nunca forçado).
 *
 * Fluxo de configuração (`etapa`): 'inicial' -> botão dispara
 * `POST /mfa/iniciar` -> 'configurando' (QR Code + segredo em `font-mono` +
 * input de código) -> `POST /mfa/confirmar` no submit. Sucesso chama
 * `atualizarUsuario` (reflete `mfaHabilitado:true` sem round-trip extra) e
 * mostra um toast (`sonner`, molde do `Toaster` já montado em `main.tsx`).
 *
 * Desligamento (Story 14.4): com MFA ativo, "Desligar meu MFA" leva a
 * `etapa 'desligando'` (senha atual + código) -> `POST /mfa/desligar`.
 * Sucesso -> `atualizarUsuario({...usuario, mfaHabilitado:false})` + toast.
 * Se `rank>=gestor && empresa.mfaObrigatorio`, o botão não aparece e fica o
 * texto do 409 MFA_EXIGIDO_PELA_EMPRESA; o servidor segue sendo a autoridade
 * (o 409 dele também vira alerta).
 */
function SegurancaCard() {
  const { usuario, atualizarUsuario } = useAuth();
  const mfaHabilitado = usuario?.mfaHabilitado ?? false;
  const mfaObrigatorio = mfaSetupPendente(usuario);
  const desligarBloqueadoPelaEmpresa =
    rankPapel(usuario?.papel ?? '') >= rankPapel('gestor') && usuario?.empresa?.mfaObrigatorio === true;

  const [etapa, setEtapa] = useState<'inicial' | 'configurando' | 'desligando'>('inicial');
  const [segredo, setSegredo] = useState('');
  const [otpauthUrl, setOtpauthUrl] = useState('');
  const [codigo, setCodigo] = useState('');
  const [senhaAtual, setSenhaAtual] = useState('');
  const [iniciando, setIniciando] = useState(false);
  const [confirmando, setConfirmando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [senhaDesligar, setSenhaDesligar] = useState('');
  const [codigoDesligar, setCodigoDesligar] = useState('');
  const [desligando, setDesligando] = useState(false);

  function abrirDesligamento() {
    setErro(null);
    setSenhaDesligar('');
    setCodigoDesligar('');
    setEtapa('desligando');
  }

  function cancelarDesligamento() {
    setEtapa('inicial');
    setErro(null);
    setSenhaDesligar('');
    setCodigoDesligar('');
  }

  async function desligarMfa(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (desligando) {
      return;
    }
    setErro(null);
    setDesligando(true);
    try {
      const res = await fetch(apiUrl('/api/auth/mfa/desligar'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ senhaAtual: senhaDesligar, codigo: codigoDesligar }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as {
          error?: { code?: string; message?: string };
        };
        if (res.status === 401) {
          setErro('Senha ou código inválido.');
        } else if (res.status === 409 && body.error?.code === 'MFA_EXIGIDO_PELA_EMPRESA') {
          setErro(body.error.message ?? MENSAGEM_MFA_EXIGIDO_PELA_EMPRESA);
        } else if (res.status === 429) {
          setErro('Muitas tentativas. Tente novamente mais tarde.');
        } else {
          setErro('Não foi possível desligar a autenticação em duas etapas agora. Tente novamente em instantes.');
        }
        return;
      }
      if (usuario) {
        atualizarUsuario({ ...usuario, mfaHabilitado: false });
      }
      setEtapa('inicial');
      setSenhaDesligar('');
      setCodigoDesligar('');
      toast.success('Autenticação em duas etapas desligada.');
    } catch {
      setErro('Não foi possível desligar a autenticação em duas etapas agora. Tente novamente em instantes.');
    } finally {
      setDesligando(false);
    }
  }

  async function iniciarConfiguracao() {
    if (iniciando) {
      return;
    }
    setErro(null);
    setIniciando(true);
    try {
      const res = await fetch(apiUrl('/api/auth/mfa/iniciar'), { method: 'POST', headers: authHeaders() });
      if (!res.ok) {
        setErro('Não foi possível iniciar a configuração agora. Tente novamente em instantes.');
        return;
      }
      const body = (await res.json()) as { segredo: string; otpauthUrl: string };
      setSegredo(body.segredo);
      setOtpauthUrl(body.otpauthUrl);
      setCodigo('');
      setSenhaAtual('');
      setEtapa('configurando');
    } catch {
      setErro('Não foi possível iniciar a configuração agora. Tente novamente em instantes.');
    } finally {
      setIniciando(false);
    }
  }

  async function confirmarConfiguracao(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (confirmando) {
      return;
    }
    setErro(null);
    setConfirmando(true);
    try {
      const res = await fetch(apiUrl('/api/auth/mfa/confirmar'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ segredo, codigo, senhaAtual }),
      });
      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as { error?: { code?: string } };
        if (body.error?.code === 'MFA_CODIGO_INVALIDO') {
          setErro('Código de autenticação inválido. Confira o código no seu aplicativo e tente novamente.');
        } else if (body.error?.code === 'INVALID_CREDENTIALS') {
          setErro('Senha atual incorreta.');
        } else {
          setErro('Não foi possível confirmar a configuração agora. Tente novamente em instantes.');
        }
        return;
      }
      if (usuario) {
        atualizarUsuario({ ...usuario, mfaHabilitado: true });
      }
      setEtapa('inicial');
      toast.success('Autenticação em duas etapas ativada com sucesso.');
    } catch {
      setErro('Não foi possível confirmar a configuração agora. Tente novamente em instantes.');
    } finally {
      setConfirmando(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Segurança</h2>
        <CardDescription>Autenticação em duas etapas (TOTP) para proteger sua conta.</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {mfaHabilitado ? (
          etapa === 'desligando' ? (
            <form onSubmit={desligarMfa} className="flex flex-col gap-4" noValidate>
              <p className="text-body text-muted-foreground">
                Para desligar, informe a sua senha atual e o código do aplicativo autenticador.
              </p>
              <div className="flex flex-col gap-2">
                <Label htmlFor="mfa-desligar-senha">Senha atual</Label>
                <Input
                  id="mfa-desligar-senha"
                  type="password"
                  autoComplete="current-password"
                  required
                  value={senhaDesligar}
                  onChange={(event) => setSenhaDesligar(event.target.value)}
                />
              </div>
              <div className="flex flex-col gap-2">
                <Label htmlFor="mfa-desligar-codigo">Código de verificação</Label>
                <Input
                  id="mfa-desligar-codigo"
                  type="text"
                  inputMode="numeric"
                  pattern="[0-9]*"
                  maxLength={6}
                  required
                  value={codigoDesligar}
                  onChange={(event) => setCodigoDesligar(event.target.value.replace(/\D/g, ''))}
                />
              </div>
              {erro && (
                <p role="alert" className="text-body text-destructive">
                  {erro}
                </p>
              )}
              <div className="flex gap-2">
                <Button type="submit" variant="destructive" disabled={desligando}>
                  {desligando ? 'Desligando...' : 'Desligar'}
                </Button>
                <Button type="button" variant="outline" disabled={desligando} onClick={cancelarDesligamento}>
                  Cancelar
                </Button>
              </div>
            </form>
          ) : (
            <>
              <p className="text-body">Autenticação em duas etapas ativa.</p>
              {desligarBloqueadoPelaEmpresa ? (
                <p className="text-body text-muted-foreground">{MENSAGEM_MFA_EXIGIDO_PELA_EMPRESA}</p>
              ) : (
                <Button type="button" variant="outline" onClick={abrirDesligamento} className="self-start">
                  Desligar meu MFA
                </Button>
              )}
              {erro && (
                <p role="alert" className="text-body text-destructive">
                  {erro}
                </p>
              )}
            </>
          )
        ) : etapa === 'inicial' ? (
          <>
            <p className="text-body text-muted-foreground">
              {mfaObrigatorio
                ? 'Obrigatório para o seu papel. Configure para continuar acessando ações restritas.'
                : 'Opcional para o seu papel.'}
            </p>
            <Button
              type="button"
              onClick={() => void iniciarConfiguracao()}
              disabled={iniciando}
              className="self-start"
            >
              {iniciando ? 'Gerando...' : 'Configurar autenticação em duas etapas'}
            </Button>
            {erro && (
              <p role="alert" className="text-body text-destructive">
                {erro}
              </p>
            )}
          </>
        ) : (
          <form onSubmit={confirmarConfiguracao} className="flex flex-col gap-4" noValidate>
            <p className="text-body text-muted-foreground">
              Escaneie o QR Code com seu aplicativo autenticador ou digite o segredo manualmente.
            </p>
            <QRCodeSVG value={otpauthUrl} size={180} />
            <div className="flex flex-col gap-1">
              <span className="text-label text-muted-foreground">Segredo</span>
              <span className="font-mono text-body break-all">{segredo}</span>
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="mfa-senha-atual">Senha atual</Label>
              <Input
                id="mfa-senha-atual"
                type="password"
                autoComplete="current-password"
                required
                value={senhaAtual}
                onChange={(event) => setSenhaAtual(event.target.value)}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="mfa-codigo">Código de verificação</Label>
              <Input
                id="mfa-codigo"
                type="text"
                inputMode="numeric"
                pattern="[0-9]*"
                maxLength={6}
                required
                value={codigo}
                onChange={(event) => setCodigo(event.target.value.replace(/\D/g, ''))}
              />
            </div>

            {erro && (
              <p role="alert" className="text-body text-destructive">
                {erro}
              </p>
            )}

            <div className="flex gap-2">
              <Button type="submit" disabled={confirmando}>
                {confirmando ? 'Confirmando...' : 'Confirmar'}
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={confirmando}
                onClick={() => {
                  setEtapa('inicial');
                  setErro(null);
                  setSenhaAtual('');
                }}
              >
                Cancelar
              </Button>
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  );
}

export function ConfiguracoesPage() {
  const { usuario } = useAuth();
  const papel = usuario?.papel ?? '';
  const alvo = proximoPapel(papel);

  const [minha, setMinha] = useState<MinhaSolicitacao | null>(null);
  const [enviando, setEnviando] = useState(false);
  const [erroSolicitar, setErroSolicitar] = useState<string | null>(null);
  const [erroCarregarMinha, setErroCarregarMinha] = useState<string | null>(null);

  const carregarMinha = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/promocoes/minha'), { headers: authHeaders() });
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

  useEffect(() => {
    void (async () => {
      await carregarMinha();
    })();
  }, [carregarMinha]);

  async function solicitar() {
    // Defesa em profundidade contra duplo-submit (molde de CadastroPage): o
    // `disabled` do botão só reflete `enviando` após o próximo repaint.
    if (enviando) {
      return;
    }
    setErroSolicitar(null);
    setEnviando(true);
    try {
      const res = await fetch(apiUrl('/api/promocoes'), { method: 'POST', headers: authHeaders() });
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

      <SegurancaCard />

      <PrivacidadeSection />
    </div>
  );
}

export default ConfiguracoesPage;
