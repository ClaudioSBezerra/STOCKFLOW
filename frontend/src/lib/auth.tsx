import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { clearAccessToken, setAccessToken } from '@/lib/session';
import { fetchSSOConfig } from '@/lib/keycloak/config';

// Marca gravada pelo callback de SSO (Story 1.9): decide se "Sair" dispara o
// RP-initiated logout do Keycloak ou só volta para /login local.
const SESSION_KEY_AUTH_VIA_SSO = 'auth_via_sso';

/**
 * Sessão de autenticação no frontend (Story 1.5, spec-1-5). O `AuthProvider`
 * envolve o router e, ao montar, faz um bootstrap SILENCIOSO da sessão a
 * partir do cookie de refresh HttpOnly (same-origin):
 *
 *   POST /api/auth/refresh  -> em 200, guarda o access token via lib/session
 *   GET  /api/auth/me       -> em 200, estado `autenticado` com o usuário
 *
 * Falha em qualquer passo -> estado `anonimo`, sem erro visível (nenhum
 * toast, nenhuma tela de erro). Enquanto os fetches não resolvem -> estado
 * `carregando`. O único gatilho de bootstrap é a montagem: não há refresh
 * proativo nem interceptor de 401 nesta story.
 */
export interface UsuarioSessao {
  id: string;
  nome: string;
  email: string;
  papel: string;
}

export type EstadoAuth = 'carregando' | 'autenticado' | 'anonimo';

interface AuthContextValue {
  estado: EstadoAuth;
  usuario: UsuarioSessao | null;
  /**
   * Promove a sessão para `autenticado` imediatamente e guarda o access
   * token — chamado por LoginPage no sucesso do login (a resposta de
   * /api/auth/login já traz `token` + `usuario`). Sem isso, `navigate('/')`
   * logo após o login cairia na rota protegida ainda `anonimo` e voltaria
   * para /login.
   */
  definirSessao: (usuario: UsuarioSessao, token: string) => void;
  /**
   * Encerra a sessão (Story 1.9): limpa o access token, volta o estado para
   * `anonimo` e dispara `POST /api/auth/logout` (best-effort, revoga a linha
   * em `sessoes`). Se a sessão veio de SSO (`sessionStorage.auth_via_sso`),
   * leva o navegador ao RP-initiated logout do Keycloak com
   * `post_logout_redirect_uri` de volta para `/login`; caso contrário vai
   * direto para `/login` sem tocar no Keycloak.
   */
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [estado, setEstado] = useState<EstadoAuth>('carregando');
  const [usuario, setUsuario] = useState<UsuarioSessao | null>(null);

  // `bootstrapIniciado` sobrevive ao unmount/remount da mesma fiber que o
  // StrictMode provoca em dev — sem isso o efeito dispararia DOIS
  // `POST /api/auth/refresh`, e como o refresh rotaciona+revoga o token
  // (Story 1.4) a segunda chamada 401aria e derrubaria a sessão para
  // `anonimo`. `sessaoManual` marca que `definirSessao` já estabeleceu a
  // sessão (login rápido enquanto o refresh ainda está pendente): o bootstrap
  // então não toca mais no estado, para o caminho de falha dele não reverter
  // `autenticado` -> `anonimo`.
  const bootstrapIniciado = useRef(false);
  const sessaoManual = useRef(false);
  // Guarda contra duplo logout (duplo clique, dois componentes de navegação
  // chamando ao mesmo tempo) — a primeira chamada já está navegando para fora.
  const saindo = useRef(false);

  const definirSessao = useCallback((u: UsuarioSessao, token: string) => {
    sessaoManual.current = true;
    setAccessToken(token);
    setUsuario(u);
    setEstado('autenticado');
  }, []);

  const logout = useCallback(() => {
    if (saindo.current) {
      return;
    }
    saindo.current = true;

    // Limpeza local SEMPRE acontece de imediato, antes de qualquer chamada de
    // rede — o app nunca fica preso esperando o backend ou o Keycloak.
    clearAccessToken();
    setUsuario(null);
    setEstado('anonimo');
    void fetch('/api/auth/logout', { method: 'POST' }).catch(() => {});

    const viaSSO = sessionStorage.getItem(SESSION_KEY_AUTH_VIA_SSO) === '1';
    if (!viaSSO) {
      window.location.assign('/login');
      return;
    }
    sessionStorage.removeItem(SESSION_KEY_AUTH_VIA_SSO);

    // Sessão SSO: RP-initiated logout no realm, com fallback para /login local
    // se a config não resolver a tempo.
    let resolvido = false;
    let timeoutId = 0;
    const irParaLoginLocal = () => {
      if (resolvido) {
        return;
      }
      resolvido = true;
      window.clearTimeout(timeoutId);
      window.location.assign('/login');
    };
    timeoutId = window.setTimeout(irParaLoginLocal, 5000);

    fetchSSOConfig()
      .then((cfg) => {
        if (resolvido) {
          return;
        }
        resolvido = true;
        window.clearTimeout(timeoutId);
        if (cfg.enabled && cfg.base_url && cfg.client_id) {
          const params = new URLSearchParams({
            client_id: cfg.client_id,
            post_logout_redirect_uri: `${window.location.origin}/login`,
          });
          window.location.assign(
            `${cfg.base_url}/protocol/openid-connect/logout?${params.toString()}`,
          );
        } else {
          window.location.assign('/login');
        }
      })
      .catch(irParaLoginLocal);
  }, []);

  useEffect(() => {
    if (bootstrapIniciado.current) {
      return;
    }
    bootstrapIniciado.current = true;

    async function bootstrap() {
      try {
        const resRefresh = await fetch('/api/auth/refresh', { method: 'POST' });
        if (!resRefresh.ok) {
          throw new Error('refresh falhou');
        }
        const { token } = (await resRefresh.json()) as { token: string };
        if (!token) {
          throw new Error('refresh sem token');
        }
        if (sessaoManual.current) {
          return;
        }
        setAccessToken(token);

        const resMe = await fetch('/api/auth/me', {
          headers: { Authorization: `Bearer ${token}` },
        });
        if (!resMe.ok) {
          throw new Error('/me falhou');
        }
        const me = (await resMe.json()) as UsuarioSessao;

        if (!sessaoManual.current) {
          setUsuario(me);
          setEstado('autenticado');
        }
      } catch {
        // Bootstrap silencioso: qualquer falha só resulta em `anonimo`. Uma
        // sessão já estabelecida por `definirSessao` (login rápido) nunca é
        // revertida.
        if (!sessaoManual.current) {
          clearAccessToken();
          setUsuario(null);
          setEstado('anonimo');
        }
      }
    }

    void bootstrap();
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ estado, usuario, definirSessao, logout }),
    [estado, usuario, definirSessao, logout],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth precisa ser usado dentro de <AuthProvider>');
  }
  return ctx;
}
