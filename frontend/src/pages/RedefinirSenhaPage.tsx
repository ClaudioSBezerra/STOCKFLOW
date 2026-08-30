import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { senhaAtendePolitica } from '@/lib/senha';

/**
 * Tela pública de redefinição de senha (Story 1.6, spec-1-6). Rota irmã de
 * `/login`, fora do `AppShell` e do `RotaProtegida`.
 *
 * Bootstrap no mount como `VerificarEmailPage`: lê `?token=` e chama
 * `GET /api/auth/redefinir-senha?token=` — que só CHECA a validade, nunca
 * consome. Sem token, ou `GET` reprovado -> estado explicativo ("link
 * expirado ou já usado" / "link inválido") com caminho para `/esqueci-senha`.
 * `GET` ok -> formulário de nova senha (um campo, `autocomplete="new-password"`).
 *
 * O `POST` continua sendo a autoridade: uma senha reprovada pela política é
 * barrada no cliente (sem chamar a API); um `TOKEN_EXPIRED`/`NOT_FOUND` no
 * submit (o token expirou entre o mount e o envio) cai no mesmo estado
 * explicativo. Sucesso -> estado final com caminho para `/login`.
 */

type Fase = 'validando' | 'formulario' | 'link-invalido' | 'link-expirado' | 'erro' | 'concluido';

interface ErroEnvelope {
  error?: { code?: string; message?: string };
}

function faseParaCodigoGet(codigo: string | undefined): Fase {
  if (codigo === 'TOKEN_EXPIRED') {
    return 'link-expirado';
  }
  if (codigo === 'NOT_FOUND') {
    return 'link-invalido';
  }
  return 'erro';
}

const mensagemExplicativa: Record<'link-invalido' | 'link-expirado' | 'erro', string> = {
  'link-invalido': 'Este link de redefinição é inválido.',
  'link-expirado': 'Este link expirou ou já foi utilizado. Solicite um novo para redefinir a senha.',
  erro: 'Não foi possível validar o link agora. Tente novamente em instantes.',
};

const MENSAGEM_SENHA_FRACA =
  'A senha deve ter ao menos 8 caracteres, incluindo uma letra e um número.';

export function RedefinirSenhaPage() {
  const [searchParams] = useSearchParams();
  const token = searchParams.get('token') ?? '';

  const [fase, setFase] = useState<Fase>(() => (token ? 'validando' : 'link-invalido'));
  const [senha, setSenha] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erroInline, setErroInline] = useState<string | null>(null);
  // Guarda o token já validado (não um booleano) — mesmo motivo de
  // VerificarEmailPage: navegar para outro `?token=` na mesma aba revalida.
  const tokenValidado = useRef<string | null>(null);
  // Marca que o fluxo já chegou ao estado final de sucesso. Um duplo-submit
  // muito rápido (antes do re-render refletir `enviando`) pode passar dois
  // POST pelo guard `if (enviando)`; o primeiro conclui e o segundo, já em
  // voo, resolveria com TOKEN_EXPIRED (token consumido) e trocaria a tela de
  // sucesso por uma de erro enganosa. Este ref barra qualquer resposta
  // perdedora de reverter `concluido`.
  const concluidoRef = useRef(false);

  useEffect(() => {
    if (!token || tokenValidado.current === token) {
      return;
    }
    tokenValidado.current = token;
    setFase('validando');

    (async () => {
      let resultante: Fase;
      try {
        const res = await fetch(`/api/auth/redefinir-senha?token=${encodeURIComponent(token)}`);
        if (res.ok) {
          resultante = 'formulario';
        } else {
          const body = (await res.json().catch(() => ({}))) as ErroEnvelope;
          resultante = faseParaCodigoGet(body.error?.code);
        }
      } catch {
        resultante = 'erro';
      }
      // Descarta resposta obsoleta se o token mudou nesse meio-tempo.
      if (tokenValidado.current === token) {
        setFase(resultante);
      }
    })();
  }, [token]);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando) {
      return;
    }
    setErroInline(null);

    // Espelho da política do backend — barra o submit sem chamar a API para
    // uma senha obviamente fraca. O backend continua sendo a autoridade.
    if (!senhaAtendePolitica(senha)) {
      setErroInline(MENSAGEM_SENHA_FRACA);
      return;
    }

    setEnviando(true);
    try {
      const res = await fetch('/api/auth/redefinir-senha', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token, senha }),
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
      if (codigo === 'TOKEN_EXPIRED' || codigo === 'NOT_FOUND') {
        // O token expirou/foi usado entre o mount e o submit — mesmo estado
        // explicativo do bootstrap.
        setFase(codigo === 'NOT_FOUND' ? 'link-invalido' : 'link-expirado');
        return;
      }
      if (codigo === 'VALIDATION_ERROR') {
        setErroInline(MENSAGEM_SENHA_FRACA);
        return;
      }
      setErroInline('Não foi possível redefinir a senha agora. Tente novamente em instantes.');
    } catch {
      if (concluidoRef.current) {
        return;
      }
      setErroInline('Não foi possível redefinir a senha agora. Tente novamente em instantes.');
    } finally {
      setEnviando(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Redefinir senha</CardTitle>
          <CardDescription>Escolha uma nova senha para a sua conta.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {fase === 'validando' && (
            <output className="text-body text-muted-foreground">Validando o link...</output>
          )}

          {fase === 'concluido' && (
            <>
              <output className="text-body">
                Senha redefinida com sucesso. Você já pode entrar com a nova senha.
              </output>
              <Button asChild className="w-full">
                <Link to="/login">Ir para o login</Link>
              </Button>
            </>
          )}

          {(fase === 'link-invalido' || fase === 'link-expirado' || fase === 'erro') && (
            <>
              <p role="alert" className="text-body text-destructive">
                {mensagemExplicativa[fase]}
              </p>
              <Button asChild className="w-full">
                <Link to="/esqueci-senha">Solicitar novo link</Link>
              </Button>
            </>
          )}

          {fase === 'formulario' && (
            <form onSubmit={handleSubmit} className="flex flex-col gap-4" noValidate>
              <div className="flex flex-col gap-2">
                <Label htmlFor="senha">Nova senha</Label>
                <Input
                  id="senha"
                  type="password"
                  autoComplete="new-password"
                  required
                  value={senha}
                  onChange={(event) => setSenha(event.target.value)}
                />
                <p className="text-body text-muted-foreground">
                  Ao menos 8 caracteres, incluindo uma letra e um número.
                </p>
              </div>

              {erroInline && (
                <p role="alert" className="text-body text-destructive">
                  {erroInline}
                </p>
              )}

              <Button type="submit" className="w-full" disabled={enviando}>
                {enviando ? 'Redefinindo...' : 'Redefinir senha'}
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

export default RedefinirSenhaPage;
