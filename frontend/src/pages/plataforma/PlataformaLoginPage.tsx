import { useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { ErroPlataforma, loginPlataforma } from '@/lib/plataforma';

const MENSAGEM_CAMPOS_OBRIGATORIOS = 'Preencha e-mail, senha e código.';

/**
 * Só o código do envelope decide o texto. INVALID_CREDENTIALS é a MESMA
 * mensagem para e-mail, senha ou código errados — o servidor nunca revela
 * qual fator falhou, e esta tela não reintroduz a distinção.
 */
function mensagemDeErro(codigo: string | undefined): string {
  if (codigo === 'INVALID_CREDENTIALS') {
    return 'E-mail, senha ou código inválidos.';
  }
  if (codigo === 'VALIDATION_ERROR') {
    return MENSAGEM_CAMPOS_OBRIGATORIOS;
  }
  return 'Não foi possível entrar. Tente novamente em instantes.';
}

/**
 * Login do Dono da Plataforma — Story 9.2 (spec-9-2). A verificação em duas
 * etapas não é opcional: e-mail, senha e o código do autenticador vão juntos
 * numa única chamada. Fora da navegação de qualquer Empresa.
 */
export function PlataformaLoginPage() {
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [senha, setSenha] = useState('');
  const [codigo, setCodigo] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando) {
      return;
    }
    setErro(null);
    if (!email.trim() || !senha || !codigo) {
      setErro(MENSAGEM_CAMPOS_OBRIGATORIOS);
      return;
    }

    setEnviando(true);
    try {
      await loginPlataforma(email, senha, codigo);
      navigate('/plataforma');
    } catch (e) {
      setErro(mensagemDeErro(e instanceof ErroPlataforma ? e.codigo : undefined));
      setCodigo('');
    } finally {
      setEnviando(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Plataforma — Entrar</CardTitle>
          <CardDescription>
            Área do Dono da Plataforma. A verificação em duas etapas é obrigatória.
          </CardDescription>
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

            <Button type="submit" className="w-full min-h-touch-target-min" disabled={enviando}>
              {enviando ? 'Entrando...' : 'Entrar'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}

export default PlataformaLoginPage;
