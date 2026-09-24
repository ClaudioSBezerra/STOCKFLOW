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
 * dela, pergunta "Ambiente real ou Treinamento?".
 *
 * Esta app NÃO monta `AuthProvider` nem router: só chama as rotas da raiz
 * e navega com `window.location.assign`.
 */
export function SemEmpresaPage() {
  const [email, setEmail] = useState('');
  const [senha, setSenha] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [avisoEsqueci, setAvisoEsqueci] = useState(false);
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
              aria-expanded={avisoEsqueci}
              onClick={() => setAvisoEsqueci(true)}
            >
              Esqueci a senha
            </Button>
            {avisoEsqueci && (
              <p className="text-body text-center text-muted-foreground">
                Para redefinir a senha, use 'Esqueci minha senha' no endereço de acesso da sua empresa.
              </p>
            )}
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

export default SemEmpresaPage;
