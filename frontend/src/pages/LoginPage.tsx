import { useEffect, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { useAuth, type UsuarioSessao } from '@/lib/auth';
import { fetchSSOConfig, type SSOConfig } from '@/lib/keycloak/config';
import { buildLoginUrl } from '@/lib/keycloak/pkce';

/**
 * Envelope de erro fixo (AD-14): {"error":{"code","message"}}. Só o código é
 * usado para decidir o texto exibido — a mensagem do backend nunca é
 * confiável para exibição direta ao usuário final (mesmo padrão de
 * CadastroPage.tsx).
 */
interface ErroEnvelope {
  error?: { code?: string; message?: string };
}

function mensagemDeErro(codigo: string | undefined): string {
  // INVALID_CREDENTIALS é deliberadamente a MESMA mensagem para todo cenário
  // de credencial inválida (senha errada, e-mail inexistente, e-mail não
  // verificado, conta desativada, conta só-SSO) — o backend nunca revela qual
  // condição falhou nem se o e-mail existe (regra explícita do contexto do
  // épico), e esta tela não pode reintroduzir essa distinção no texto.
  if (codigo === 'INVALID_CREDENTIALS') {
    return 'E-mail ou senha inválidos.';
  }
  if (codigo === 'VALIDATION_ERROR') {
    return 'Preencha e-mail e senha para continuar.';
  }
  // ACCOUNT_LOCKED (Story 1.10): conta bloqueada após 5 tentativas falhas. A
  // mensagem NUNCA revela o tempo restante e NÃO promete que redefinir a senha
  // destrava a conta — só a expiração do prazo faz isso (RedefinirSenha não
  // toca nas colunas de bloqueio). O link "Esqueci minha senha" segue visível
  // no formulário para quem de fato esqueceu a senha.
  if (codigo === 'ACCOUNT_LOCKED') {
    return 'Muitas tentativas de login sem sucesso. Por segurança, novas tentativas ficam bloqueadas temporariamente. Tente novamente mais tarde.';
  }
  // MFA_CODIGO_INVALIDO/MFA_TOKEN_INVALIDO (Story 1.11): segunda etapa do
  // login, POST /api/auth/mfa/verificar.
  if (codigo === 'MFA_CODIGO_INVALIDO') {
    return 'Código de autenticação inválido.';
  }
  if (codigo === 'MFA_TOKEN_INVALIDO') {
    return 'Código de login expirado. Faça login novamente.';
  }
  return 'Não foi possível entrar. Tente novamente em instantes.';
}

interface LoginSucesso {
  token: string;
  usuario: UsuarioSessao;
}

/**
 * Resposta de POST /api/auth/login quando a conta tem MFA habilitado (Story
 * 1.11): nenhuma sessão é emitida ainda — `mfaToken` é trocado por sessão em
 * POST /api/auth/mfa/verificar.
 */
interface LoginMFARequerida {
  mfaRequerido: true;
  mfaToken: string;
}

type LoginResposta = LoginSucesso | LoginMFARequerida;

function ehMFARequerida(body: LoginResposta): body is LoginMFARequerida {
  return (body as LoginMFARequerida).mfaRequerido === true;
}

/**
 * Tela pública de login (Story 1.4, spec-1-4). Rota irmã de `/cadastro` e
 * `/verificar-email`, fora do `AppShell` — mesmo layout mínimo (Story 1.3).
 * Login bem-sucedido chama `definirSessao(usuario, token)` do `AuthProvider`
 * (Story 1.5), que guarda o access token em memória (nunca
 * `localStorage`/`sessionStorage`) e promove o estado para `autenticado`
 * ANTES do `navigate('/')` — sem isso, a rota protegida ainda veria
 * `anonimo` e devolveria para `/login`. O refresh token chega em cookie
 * HttpOnly, fora do alcance desta tela.
 */
export function LoginPage() {
  const navigate = useNavigate();
  const { definirSessao } = useAuth();
  const [email, setEmail] = useState('');
  const [senha, setSenha] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  // Config de SSO buscada em runtime (Story 1.9): começa oculto; o botão
  // "Entrar com Ferreira Costa" só aparece quando o backend responde
  // `enabled:true`. O fluxo de senha nunca muda e NUNCA há auto-redirect.
  const [ssoConfig, setSSOConfig] = useState<SSOConfig | null>(null);

  // Segundo fator (Story 1.11): `etapa` alterna entre o formulário de
  // e-mail/senha e o de código TOTP. `mfaToken` só existe durante a etapa
  // `codigo` — é o token opaco de uso único devolvido por
  // POST /api/auth/login quando a conta tem MFA habilitado, trocado por
  // sessão em POST /api/auth/mfa/verificar.
  const [etapa, setEtapa] = useState<'senha' | 'codigo'>('senha');
  const [mfaToken, setMfaToken] = useState('');
  const [codigo, setCodigo] = useState('');

  useEffect(() => {
    void fetchSSOConfig().then(setSSOConfig);
  }, []);

  async function iniciarLoginSSO() {
    if (!ssoConfig?.enabled) {
      return;
    }
    try {
      const url = await buildLoginUrl(ssoConfig);
      window.location.assign(url);
    } catch {
      // Config incompleta: mantém apenas o login por senha, sem quebrar a tela.
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    // Defesa em profundidade contra duplo-submit: mesmo guard de
    // CadastroPage.tsx — o atributo `disabled` do botão só reflete `enviando`
    // depois do próximo repaint do React.
    if (enviando) {
      return;
    }
    setErro(null);
    setEnviando(true);

    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, senha }),
      });

      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as ErroEnvelope;
        setErro(mensagemDeErro(body.error?.code));
        return;
      }

      const body = (await res.json()) as LoginResposta;
      if (ehMFARequerida(body)) {
        setMfaToken(body.mfaToken);
        setCodigo('');
        setEtapa('codigo');
        return;
      }
      definirSessao(body.usuario, body.token);
      navigate('/');
    } catch {
      setErro(mensagemDeErro(undefined));
    } finally {
      setEnviando(false);
    }
  }

  async function handleSubmitCodigo(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando) {
      return;
    }
    setErro(null);
    setEnviando(true);

    try {
      const res = await fetch('/api/auth/mfa/verificar', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mfaToken, codigo }),
      });

      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as ErroEnvelope;
        // Token expirado/inválido (mfaToken de 5min já venceu, ou já foi
        // trocado por sessão em outra aba): volta para a etapa de senha — o
        // token desta etapa não presta mais para nada.
        if (body.error?.code === 'MFA_TOKEN_INVALIDO') {
          setEtapa('senha');
          setSenha('');
        }
        setErro(mensagemDeErro(body.error?.code));
        return;
      }

      const body = (await res.json()) as LoginSucesso;
      definirSessao(body.usuario, body.token);
      navigate('/');
    } catch {
      setErro(mensagemDeErro(undefined));
    } finally {
      setEnviando(false);
    }
  }

  if (etapa === 'codigo') {
    return (
      <div className="flex min-h-screen items-center justify-center p-6">
        <Card className="w-full max-w-sm">
          <CardHeader>
            <CardTitle>Verificação em duas etapas</CardTitle>
            <CardDescription>
              Digite o código de 6 dígitos do seu aplicativo autenticador.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={handleSubmitCodigo} className="flex flex-col gap-4" noValidate>
              <div className="flex flex-col gap-2">
                <Label htmlFor="codigo">Código de verificação</Label>
                <Input
                  id="codigo"
                  type="text"
                  inputMode="numeric"
                  autoComplete="one-time-code"
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

              <Button type="submit" className="w-full" disabled={enviando}>
                {enviando ? 'Verificando...' : 'Verificar'}
              </Button>

              <Button
                type="button"
                variant="ghost"
                className="w-full"
                disabled={enviando}
                onClick={() => {
                  setEtapa('senha');
                  setSenha('');
                  setErro(null);
                }}
              >
                Voltar
              </Button>
            </form>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Entrar</CardTitle>
          <CardDescription>Acesse sua conta do stockflow.</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="flex flex-col gap-4" noValidate>
            <div className="flex flex-col gap-2">
              <Label htmlFor="email">E-mail</Label>
              <Input
                id="email"
                type="email"
                autoComplete="email"
                required
                value={email}
                onChange={(event) => setEmail(event.target.value)}
              />
            </div>
            <div className="flex flex-col gap-2">
              <Label htmlFor="senha">Senha</Label>
              <Input
                id="senha"
                type="password"
                autoComplete="current-password"
                required
                value={senha}
                onChange={(event) => setSenha(event.target.value)}
              />
            </div>

            {erro && (
              <p role="alert" className="text-body text-destructive">
                {erro}
              </p>
            )}

            <Button type="submit" className="w-full" disabled={enviando}>
              {enviando ? 'Entrando...' : 'Entrar'}
            </Button>

            <p className="text-body text-center text-muted-foreground">
              <Link to="/esqueci-senha" className="text-primary hover:underline">
                Esqueci minha senha
              </Link>
            </p>

            <p className="text-body text-center text-muted-foreground">
              Ainda não tem uma conta?{' '}
              <Link to="/cadastro" className="text-primary hover:underline">
                Criar conta
              </Link>
            </p>

            {ssoConfig?.enabled ? (
              <Button
                type="button"
                variant="outline"
                className="w-full"
                onClick={() => void iniciarLoginSSO()}
              >
                Entrar com Ferreira Costa
              </Button>
            ) : null}
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

export default LoginPage;
