import { useEffect, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';

type Estado = 'carregando' | 'sucesso' | 'expirado' | 'nao-encontrado' | 'erro';

interface ErroEnvelope {
  error?: { code?: string; message?: string };
}

function estadoParaCodigo(codigo: string | undefined): Estado {
  if (codigo === 'TOKEN_EXPIRED') {
    return 'expirado';
  }
  if (codigo === 'NOT_FOUND') {
    return 'nao-encontrado';
  }
  return 'erro';
}

const mensagens: Record<Exclude<Estado, 'carregando'>, string> = {
  sucesso: 'E-mail verificado com sucesso. Sua conta já está pronta para uso.',
  expirado: 'Este link expirou ou já foi utilizado. Solicite um novo cadastro para gerar outro link.',
  'nao-encontrado': 'Link de verificação inválido.',
  erro: 'Não foi possível verificar seu e-mail agora. Tente novamente em instantes.',
};

/**
 * Tela pública que processa o link recebido por e-mail (Story 1.3,
 * spec-1-3). Lê `?token=` da URL e consome GET /api/auth/verificar-email ao
 * montar — rota irmã da raiz do `AppShell`, fora dele.
 */
export function VerificarEmailPage() {
  const [searchParams] = useSearchParams();
  const token = searchParams.get('token') ?? '';
  // Sem token na URL o estado final já é conhecido antes de qualquer efeito
  // rodar — inicializar direto aqui evita um setState síncrono dentro do
  // useEffect só para cobrir esse caso.
  const [estado, setEstado] = useState<Estado>(() => (token ? 'carregando' : 'nao-encontrado'));
  // Guarda o token já verificado (não um booleano) para que, se o usuário
  // navegar para um `?token=` diferente sem recarregar a página (ex. dois
  // links de e-mail abertos na mesma aba via navegação client-side), o
  // efeito rode de novo em vez de ficar preso ao resultado do primeiro token.
  const tokenVerificado = useRef<string | null>(null);

  useEffect(() => {
    if (!token || tokenVerificado.current === token) {
      return;
    }
    tokenVerificado.current = token;
    setEstado('carregando');

    (async () => {
      let estadoResultante: Estado;
      try {
        const res = await fetch(`/api/auth/verificar-email?token=${encodeURIComponent(token)}`);
        if (res.ok) {
          estadoResultante = 'sucesso';
        } else {
          const body = (await res.json().catch(() => ({}))) as ErroEnvelope;
          estadoResultante = estadoParaCodigo(body.error?.code);
        }
      } catch {
        estadoResultante = 'erro';
      }
      // Se o usuário já trocou de token (novo link aberto na mesma aba) antes
      // desta resposta chegar, tokenVerificado.current aponta para o token
      // novo — descarta a resposta obsoleta em vez de sobrescrever o estado
      // já em andamento/concluído para o token atual.
      if (tokenVerificado.current === token) {
        setEstado(estadoResultante);
      }
    })();
  }, [token]);

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Verificação de e-mail</CardTitle>
          <CardDescription>Confirmando o link recebido por e-mail.</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {estado === 'carregando' ? (
            <output className="text-body text-muted-foreground">Verificando...</output>
          ) : estado === 'sucesso' ? (
            <output className="text-body">{mensagens[estado]}</output>
          ) : (
            <p role="alert" className="text-body text-destructive">
              {mensagens[estado]}
            </p>
          )}

          {estado !== 'carregando' && (
            <Button asChild className="w-full">
              <Link to={estado === 'sucesso' ? '/' : '/cadastro'}>
                {estado === 'sucesso' ? 'Ir para o início' : 'Voltar ao cadastro'}
              </Link>
            </Button>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

export default VerificarEmailPage;
