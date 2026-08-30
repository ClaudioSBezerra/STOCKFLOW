import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';

/**
 * Tela pública "Esqueci minha senha" (Story 1.6, spec-1-6). Rota irmã de
 * `/login`, fora do `AppShell` e do `RotaProtegida` — molde de
 * `CadastroPage.tsx`: um campo, submit, estado de sucesso genérico.
 *
 * O backend responde SEMPRE a mesma mensagem genérica (exista ou não a
 * conta), então qualquer `2xx` aqui vira o mesmo estado de sucesso — a tela
 * nunca revela se o e-mail está cadastrado. Só um erro de rede/servidor
 * mostra uma mensagem de nova tentativa.
 */
export function EsqueciSenhaPage() {
  const [email, setEmail] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [enviado, setEnviado] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    // Mesmo guard de duplo-submit de CadastroPage/LoginPage: o `disabled` do
    // botão só reflete `enviando` após o próximo repaint.
    if (enviando) {
      return;
    }
    setErro(null);
    setEnviando(true);

    try {
      const res = await fetch('/api/auth/esqueci-senha', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email }),
      });

      if (!res.ok) {
        setErro('Não foi possível enviar o link agora. Tente novamente em instantes.');
        return;
      }

      setEnviado(true);
    } catch {
      setErro('Não foi possível enviar o link agora. Tente novamente em instantes.');
    } finally {
      setEnviando(false);
    }
  }

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
          {enviado ? (
            <div className="flex flex-col gap-4">
              {/* Texto EXATO da AC1 do épico e byte-idêntico à constante
                  `mensagemEsqueciSenha` do backend — não parafrasear. */}
              <output className="text-body">Se o e-mail existir, você receberá um link.</output>
              <Button asChild variant="outline" className="w-full">
                <Link to="/login">Voltar para o login</Link>
              </Button>
            </div>
          ) : (
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

              {erro && (
                <p role="alert" className="text-body text-destructive">
                  {erro}
                </p>
              )}

              <Button type="submit" className="w-full" disabled={enviando}>
                {enviando ? 'Enviando...' : 'Enviar link de redefinição'}
              </Button>

              <p className="text-body text-center text-muted-foreground">
                Lembrou a senha?{' '}
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

export default EsqueciSenhaPage;
