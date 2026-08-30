import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { senhaAtendePolitica } from '@/lib/senha';

/**
 * Envelope de erro fixo (AD-14): {"error":{"code","message"}}. Só o código é
 * usado para decidir o texto exibido — a mensagem do backend nunca é
 * confiável para exibição direta ao usuário final.
 */
interface ErroEnvelope {
  error?: { code?: string; message?: string };
}

/**
 * Critério da política mínima de força de senha (Story 1.10). String idêntica
 * à de RedefinirSenhaPage.tsx — o backend usa a MESMA mensagem em
 * CadastroHandler e RedefinirSenhaHandler.
 */
const MENSAGEM_SENHA_FRACA =
  'A senha deve ter ao menos 8 caracteres, incluindo uma letra e um número.';

const MENSAGEM_CAMPOS_OBRIGATORIOS = 'Preencha nome, e-mail e senha para continuar.';

function mensagemDeErro(codigo: string | undefined): string {
  if (codigo === 'CONFLICT') {
    return 'Este e-mail já está cadastrado.';
  }
  // O backend usa VALIDATION_ERROR tanto para "campo obrigatório" quanto para
  // "senha fraca". `handleSubmit` já barra campos vazios no cliente antes de
  // chamar a API, então um VALIDATION_ERROR que chega aqui só pode ser senha
  // fraca (ou divergência do espelho `senhaAtendePolitica`) — mostrar o
  // critério da política é sempre a mensagem correta. Molde de
  // RedefinirSenhaPage.tsx.
  if (codigo === 'VALIDATION_ERROR') {
    return MENSAGEM_SENHA_FRACA;
  }
  return 'Não foi possível concluir o cadastro. Tente novamente em instantes.';
}

/**
 * Tela pública de autocadastro (Story 1.3, spec-1-3). Rota irmã da raiz do
 * `AppShell`, fora dele (EXPERIENCE.md: mesma classificação de superfície
 * pública do Login, ainda não implementado). Sempre cria a conta como
 * `usuario` no backend — nenhum campo de papel existe neste formulário.
 */
export function CadastroPage() {
  const [nome, setNome] = useState('');
  const [email, setEmail] = useState('');
  const [senha, setSenha] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [sucesso, setSucesso] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    // Defesa em profundidade contra duplo-submit: o atributo `disabled` do
    // botão só reflete `enviando` depois do próximo repaint do React, então
    // um clique/Enter duplo bem rápido, antes desse repaint, ainda chegaria
    // aqui. Checar `enviando` diretamente fecha essa janela.
    if (enviando) {
      return;
    }
    setErro(null);

    // Campos obrigatórios barrados no cliente com mensagem própria: sem isto,
    // o VALIDATION_ERROR de campo vazio vindo do backend cairia no mapeamento
    // de "senha fraca" abaixo e mostraria o critério de senha com os campos em
    // branco.
    if (!nome.trim() || !email.trim() || !senha) {
      setErro(MENSAGEM_CAMPOS_OBRIGATORIOS);
      return;
    }

    // Espelho da política do backend (molde de RedefinirSenhaPage): barra o
    // submit sem chamar a API para uma senha obviamente fraca. O backend
    // continua sendo a autoridade (revalida e devolve VALIDATION_ERROR).
    if (!senhaAtendePolitica(senha)) {
      setErro(MENSAGEM_SENHA_FRACA);
      return;
    }

    setEnviando(true);

    try {
      const res = await fetch('/api/auth/cadastro', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ nome, email, senha }),
      });

      if (!res.ok) {
        const body = (await res.json().catch(() => ({}))) as ErroEnvelope;
        setErro(mensagemDeErro(body.error?.code));
        return;
      }

      setSucesso(true);
    } catch {
      setErro(mensagemDeErro(undefined));
    } finally {
      setEnviando(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Crie sua conta</CardTitle>
          <CardDescription>Cadastre-se para acessar o stockflow.</CardDescription>
        </CardHeader>
        <CardContent>
          {sucesso ? (
            <output className="text-body">Verifique seu e-mail para confirmar a conta.</output>
          ) : (
            <form onSubmit={handleSubmit} className="flex flex-col gap-4" noValidate>
              <div className="flex flex-col gap-2">
                <Label htmlFor="nome">Nome</Label>
                <Input
                  id="nome"
                  autoComplete="name"
                  required
                  value={nome}
                  onChange={(event) => setNome(event.target.value)}
                />
              </div>
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
                  autoComplete="new-password"
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
                {enviando ? 'Criando conta...' : 'Criar conta'}
              </Button>

              <p className="text-body text-center text-muted-foreground">
                Já tem uma conta?{' '}
                <Link to="/login" className="text-primary hover:underline">
                  Entrar
                </Link>
              </p>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

export default CadastroPage;
