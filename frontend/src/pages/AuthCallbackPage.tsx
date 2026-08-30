import { useEffect, useRef, useState } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '@/lib/auth';
import { ErroCallbackSSO, trocarCodePorSessao } from '@/lib/keycloak/callback';

/**
 * Tela de transição do login federado (Story 1.9) — rota pública
 * `/auth/callback`, fora do `AppShell`/`RotaProtegida`. No mount (com guarda
 * `useRef` contra o duplo-efeito do StrictMode) troca o `code` do Keycloak por
 * uma sessão própria; sucesso -> marca `auth_via_sso`, `definirSessao` e vai
 * para `/`. Falha -> mensagem de erro por `codigo`, sem nunca estabelecer
 * sessão e sem redirecionar de volta ao Keycloak automaticamente.
 */

interface TextoErro {
  texto: string;
  mostrarCadastro: boolean;
}

function textoPorCodigo(codigo: string): TextoErro {
  if (codigo === 'SSO_SEM_CONTA') {
    return {
      texto:
        'Não encontramos uma conta do stockflow para este e-mail corporativo. Cadastre-se primeiro para entrar pelo Ferreira Costa.',
      mostrarCadastro: true,
    };
  }
  if (codigo === 'EMAIL_NOT_VERIFIED') {
    return {
      texto:
        'Seu e-mail corporativo ainda não está confirmado no Ferreira Costa. Confirme-o e tente entrar novamente.',
      mostrarCadastro: false,
    };
  }
  if (codigo === 'CSRF') {
    return {
      texto:
        'Não foi possível validar o retorno do login (possível CSRF). Inicie o login novamente.',
      mostrarCadastro: false,
    };
  }
  return {
    texto: 'Não foi possível concluir o login via Ferreira Costa. Tente novamente.',
    mostrarCadastro: false,
  };
}

export function AuthCallbackPage() {
  const navigate = useNavigate();
  const { definirSessao } = useAuth();
  const [codigoErro, setCodigoErro] = useState<string | null>(null);
  const iniciado = useRef(false);

  useEffect(() => {
    if (iniciado.current) {
      return;
    }
    iniciado.current = true;

    trocarCodePorSessao(new URLSearchParams(window.location.search))
      .then(({ token, usuario }) => {
        sessionStorage.setItem('auth_via_sso', '1');
        definirSessao(usuario, token);
        navigate('/', { replace: true });
      })
      .catch((e: unknown) => {
        setCodigoErro(e instanceof ErroCallbackSSO ? e.codigo : 'DESCONHECIDO');
      });
  }, [definirSessao, navigate]);

  if (codigoErro) {
    const { texto, mostrarCadastro } = textoPorCodigo(codigoErro);
    return (
      <div className="flex min-h-screen items-center justify-center p-6">
        <div className="flex w-full max-w-sm flex-col items-center gap-4 text-center">
          <p role="alert" className="text-body text-destructive">
            {texto}
          </p>
          {mostrarCadastro ? (
            <Link to="/cadastro" className="text-primary hover:underline">
              Criar conta
            </Link>
          ) : null}
          <Link to="/login" className="text-primary hover:underline">
            Voltar para o login
          </Link>
        </div>
      </div>
    );
  }

  return (
    <output className="flex min-h-screen items-center justify-center p-6 text-muted-foreground">
      Concluindo login via Ferreira Costa...
    </output>
  );
}

export default AuthCallbackPage;
