import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { senhaAtendePolitica } from '@/lib/senha';
import { apiUrl } from '@/lib/api';

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

const MENSAGEM_CAMPOS_OBRIGATORIOS = 'Preencha nome e senha para continuar.';

/**
 * Fases da tela (molde de RedefinirSenhaPage): o cadastro deixou de ser uma
 * página sempre-aberta e passou a depender de um convite válido, então o
 * `?token=` é validado no mount e cada motivo de recusa tem um estado
 * explicativo próprio.
 */
type Fase =
  | 'validando'
  | 'formulario'
  | 'sem-convite'
  | 'convite-invalido'
  | 'convite-expirado'
  | 'convite-usado'
  | 'convite-cancelado'
  | 'erro'
  | 'concluido';

/**
 * Tradução dos códigos do envelope de erro nas fases explicativas. A MESMA
 * tabela vale para o `GET` de validação e para o `POST` de cadastro: o
 * backend usa o mesmo vocabulário nos dois (ValidarConviteHandler e
 * CadastroHandler), então um convite que morre entre abrir a tela e enviar o
 * formulário cai exatamente no mesmo estado.
 */
function faseParaCodigo(codigo: string | undefined): Fase {
  switch (codigo) {
    case 'NOT_FOUND':
      return 'convite-invalido';
    case 'TOKEN_EXPIRED':
      return 'convite-expirado';
    case 'CONFLICT':
      return 'convite-usado';
    case 'FORBIDDEN':
      return 'convite-cancelado';
    default:
      return 'erro';
  }
}

type FaseExplicativa =
  | 'sem-convite'
  | 'convite-invalido'
  | 'convite-expirado'
  | 'convite-usado'
  | 'convite-cancelado'
  | 'erro';

const mensagemExplicativa: Record<FaseExplicativa, string> = {
  'sem-convite':
    'Para criar uma conta você precisa de um convite da empresa. Peça o link a um gestor.',
  'convite-invalido': 'Este convite não é válido. Peça um novo link a um gestor da empresa.',
  'convite-expirado': 'Este convite expirou. Peça um novo link a um gestor da empresa.',
  'convite-usado': 'Este convite já foi utilizado. Se a conta já existe, entre pelo login.',
  'convite-cancelado': 'Este convite foi cancelado pela empresa.',
  erro: 'Não foi possível validar o convite agora. Tente novamente em instantes.',
};

function ehFaseExplicativa(fase: Fase): fase is FaseExplicativa {
  return fase in mensagemExplicativa;
}

/**
 * Tela pública de autocadastro (Story 1.3, spec-1-3; convite obrigatório na
 * Story 9.3, spec-9-3). Rota irmã da raiz do `AppShell`, fora dele.
 *
 * O autocadastro NÃO é mais aberto: a tela exige `?token=`, valida-o no mount
 * com `GET /api/auth/convite?token=` (que só CHECA, nunca consome) e
 * pré-preenche o e-mail convidado em um campo somente-leitura. Sem token, ou
 * com um convite inválido/expirado/usado/cancelado, o formulário nem aparece
 * — só o estado explicativo com caminho para `/login`.
 *
 * A conta continua nascendo sempre como `usuario` no backend — nenhum campo
 * de papel existe neste formulário — e a Empresa vem sempre do convite/slug,
 * nunca de um campo. O campo somente-leitura é conveniência contra erro de
 * digitação: o servidor revalida o e-mail contra o convite no POST.
 */
export function CadastroPage() {
  const [searchParams] = useSearchParams();
  const token = searchParams.get('token') ?? '';

  const [fase, setFase] = useState<Fase>(() => (token ? 'validando' : 'sem-convite'));
  const [nome, setNome] = useState('');
  const [email, setEmail] = useState('');
  const [senha, setSenha] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  // Guarda o token já validado (não um booleano) — mesmo motivo de
  // RedefinirSenhaPage: navegar para outro `?token=` na mesma aba revalida.
  const tokenValidado = useRef<string | null>(null);
  // Marca que o fluxo já chegou ao estado final de sucesso: uma resposta
  // perdedora de um POST concorrente nunca pode reverter a tela de sucesso
  // (mesmo guard de RedefinirSenhaPage).
  const concluidoRef = useRef(false);

  useEffect(() => {
    if (!token || tokenValidado.current === token) {
      return;
    }
    tokenValidado.current = token;
    setFase('validando');

    void (async () => {
      let resultante: Fase;
      let emailConvidado = '';
      try {
        const res = await fetch(apiUrl(`/api/auth/convite?token=${encodeURIComponent(token)}`));
        if (res.ok) {
          const body = (await res.json().catch(() => ({}))) as { email?: string };
          emailConvidado = body.email ?? '';
          resultante = 'formulario';
        } else {
          const body = (await res.json().catch(() => ({}))) as ErroEnvelope;
          resultante = faseParaCodigo(body.error?.code);
        }
      } catch {
        resultante = 'erro';
      }
      // Descarta resposta obsoleta se o token mudou nesse meio-tempo.
      if (tokenValidado.current === token) {
        setEmail(emailConvidado);
        setFase(resultante);
      }
    })();
  }, [token]);

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
    // branco. O e-mail não entra aqui: ele é somente-leitura e vem do convite.
    if (!nome.trim() || !senha) {
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
      const res = await fetch(apiUrl('/api/auth/cadastro'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token, nome, email, senha }),
      });

      if (res.ok) {
        concluidoRef.current = true;
        setFase('concluido');
        return;
      }

      // Resposta perdedora de um POST concorrente: o fluxo já concluiu com
      // sucesso — nada aqui pode reverter o estado final.
      if (concluidoRef.current) {
        return;
      }

      const body = (await res.json().catch(() => ({}))) as ErroEnvelope;
      const codigo = body.error?.code;
      // VALIDATION_ERROR é o único erro que mantém o formulário na tela: é do
      // payload (senha fraca / divergência do espelho), não do convite. Todos
      // os outros códigos significam que o convite morreu entre o mount e o
      // envio, e a tela vira o estado explicativo correspondente.
      if (codigo === 'VALIDATION_ERROR') {
        setErro(MENSAGEM_SENHA_FRACA);
        return;
      }
      setFase(faseParaCodigo(codigo));
    } catch {
      if (concluidoRef.current) {
        return;
      }
      setErro('Não foi possível concluir o cadastro. Tente novamente em instantes.');
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
        <CardContent className="flex flex-col gap-4">
          {fase === 'validando' && (
            <output className="text-body text-muted-foreground">Validando o convite...</output>
          )}

          {fase === 'concluido' && (
            <output className="text-body">Verifique seu e-mail para confirmar a conta.</output>
          )}

          {ehFaseExplicativa(fase) && (
            <>
              <p role="alert" className="text-body text-destructive">
                {mensagemExplicativa[fase]}
              </p>
              <Button asChild className="w-full">
                <Link to="/login">Ir para o login</Link>
              </Button>
            </>
          )}

          {fase === 'formulario' && (
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
                {/* Somente-leitura: o e-mail é o do convite. O servidor
                    revalida no POST — este campo é conveniência contra erro de
                    digitação, nunca a barreira. */}
                <Input id="email" type="email" autoComplete="email" readOnly value={email} />
                <p className="text-body text-muted-foreground">
                  Este convite foi emitido para este e-mail.
                </p>
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
