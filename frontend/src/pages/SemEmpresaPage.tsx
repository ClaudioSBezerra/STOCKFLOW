import { useState, type FormEvent } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import {
  concluirEscolha,
  entrarPelaConta,
  gravarMfaPendente,
  mensagemDeErroLogin,
  pedirRedefinicaoPelaConta,
  type OpcaoEscolha,
  type ResultadoEntrada,
} from '@/lib/entrada';

/**
 * Login pela conta na raiz do domínio — Story 15.2 (AD-36). Servida para
 * qualquer caminho fora de `/e/{empresa}` e `/plataforma`. E-mail e senha
 * descobrem a Empresa da conta; a pessoa é levada para `/e/{slug}/`, onde o
 * `AuthProvider` restaura a sessão pelo refresh silencioso (o cookie já veio
 * com `Path=/e/{slug}/api/auth`). Com MFA, o código é digitado em
 * `/e/{slug}/login`. Quando a senha confere na Empresa real e no Treinamento
 * dela, pergunta "Ambiente real ou Treinamento?". "Esqueci a senha" (Story
 * 15.3) abre o pedido de redefinição pela conta, sem Empresa na URL.
 *
 * Esta app NÃO monta `AuthProvider` nem router: só chama as rotas da raiz
 * e navega com `window.location.assign`.
 */
export function SemEmpresaPage() {
  const [email, setEmail] = useState('');
  const [senha, setSenha] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [esqueci, setEsqueci] = useState<{ email: string; enviado: boolean; erro: string | null } | null>(
    null,
  );
  const [escolha, setEscolha] = useState<{ opcoes: OpcaoEscolha[]; token: string } | null>(null);

  function seguir(resultado: ResultadoEntrada) {
    switch (resultado.tipo) {
      case 'sessao':
        window.location.assign(`/e/${resultado.slug}/`);
        return;
      case 'mfa':
        gravarMfaPendente(resultado.slug, resultado.mfaToken);
        window.location.assign(`/e/${resultado.slug}/login`);
        return;
      case 'escolha':
        setEscolha({ opcoes: resultado.escolha, token: resultado.escolhaToken });
        return;
      case 'erro':
        if (resultado.codigo === 'ESCOLHA_INVALIDA') {
          setEscolha(null);
          setSenha('');
        }
        setErro(mensagemDeErroLogin(resultado.codigo));
        setEnviando(false);
        return;
    }
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando) {
      return;
    }
    setErro(null);
    setEnviando(true);
    const resultado = await entrarPelaConta(email, senha);
    if (resultado.tipo === 'escolha') {
      setEnviando(false);
    }
    seguir(resultado);
  }

  async function escolher(slug: string) {
    if (enviando || !escolha) {
      return;
    }
    setErro(null);
    setEnviando(true);
    seguir(await concluirEscolha(escolha.token, slug));
  }

  async function pedirRedefinicao(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando || !esqueci) {
      return;
    }
    setEsqueci({ ...esqueci, erro: null });
    setEnviando(true);
    // O backend responde sempre a mesma mensagem (exista ou não a conta):
    // qualquer 2xx vira o mesmo estado de sucesso.
    const ok = await pedirRedefinicaoPelaConta(esqueci.email);
    setEnviando(false);
    setEsqueci((atual) =>
      atual && {
        ...atual,
        enviado: ok,
        erro: ok ? null : 'Não foi possível enviar o link agora. Tente novamente em instantes.',
      },
    );
  }

  function voltarDoEsqueci() {
    // Leva de volta ao login o e-mail corrigido nesta etapa.
    if (esqueci) {
      setEmail(esqueci.email);
    }
    setEsqueci(null);
    setErro(null);
  }

  if (esqueci) {
    return (
      <div className="flex min-h-screen items-center justify-center p-6">
        <Card className="w-full max-w-sm">
          <CardHeader>
            <CardTitle>Esqueci minha senha</CardTitle>
            <CardDescription>
              Informe seu e-mail e enviaremos um link para redefinir a senha.
            </CardDescription>
          </CardHeader>
          <CardContent>
            {esqueci.enviado ? (
              <div className="flex flex-col gap-4">
                {/* Byte-idêntico à constante `mensagemEsqueciSenha` do backend. */}
                <output className="text-body">Se o e-mail existir, você receberá um link.</output>
                <Button type="button" variant="outline" className="w-full" onClick={voltarDoEsqueci}>
                  Voltar para o login
                </Button>
              </div>
            ) : (
              <form onSubmit={pedirRedefinicao} className="flex flex-col gap-4" noValidate>
                <div className="flex flex-col gap-2">
                  <Label htmlFor="email-esqueci">E-mail</Label>
                  <Input
                    id="email-esqueci"
                    type="email"
                    autoComplete="email"
                    required
                    disabled={enviando}
                    value={esqueci.email}
                    onChange={(event) => setEsqueci({ ...esqueci, email: event.target.value })}
                  />
                </div>

                {esqueci.erro && (
                  <p role="alert" className="text-body text-destructive">
                    {esqueci.erro}
                  </p>
                )}

                <Button type="submit" className="w-full" disabled={enviando}>
                  {enviando ? 'Enviando...' : 'Enviar link de redefinição'}
                </Button>

                <Button
                  type="button"
                  variant="ghost"
                  className="w-full"
                  disabled={enviando}
                  onClick={voltarDoEsqueci}
                >
                  Voltar para o login
                </Button>
              </form>
            )}
          </CardContent>
        </Card>
      </div>
    );
  }

  if (escolha) {
    return (
      <div className="flex min-h-screen items-center justify-center p-6">
        <Card className="w-full max-w-sm">
          <CardHeader>
            <CardTitle>Ambiente real ou Treinamento?</CardTitle>
            <CardDescription>Sua conta existe nos dois ambientes. Escolha onde entrar.</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3">
            {escolha.opcoes.map((opcao) => (
              <Button
                key={opcao.slug}
                type="button"
                variant={opcao.treinamento ? 'outline' : 'default'}
                className="h-auto w-full flex-col items-center gap-0.5 py-3"
                disabled={enviando}
                onClick={() => void escolher(opcao.slug)}
              >
                <span>{opcao.nomeFantasia}</span>
                <span className="text-label font-normal">
                  {opcao.treinamento ? 'Treinamento' : 'Ambiente real'}
                </span>
              </Button>
            ))}

            {erro && (
              <p role="alert" className="text-body text-destructive">
                {erro}
              </p>
            )}

            <Button
              type="button"
              variant="ghost"
              className="w-full"
              disabled={enviando}
              onClick={() => {
                setEscolha(null);
                setSenha('');
                setErro(null);
              }}
            >
              Voltar
            </Button>
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
          <CardDescription>Acesse sua conta com seu e-mail e senha.</CardDescription>
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

            <Button
              type="button"
              variant="link"
              className="w-full"
              disabled={enviando}
              onClick={() => {
                setErro(null);
                setEsqueci({ email, enviado: false, erro: null });
              }}
            >
              Esqueci a senha
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

export default SemEmpresaPage;
