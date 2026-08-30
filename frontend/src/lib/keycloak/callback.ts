/**
 * Troca o `code` do callback OIDC por uma sessão própria do stockflow — Story 1.9.
 *
 * O access token do Keycloak só vive o tempo desta função: depois de validado
 * pelo backend (`POST /api/auth/sso/keycloak`, atrás de `iam.Middleware`), a
 * resposta já vem no mesmo shape do login por senha (`{token, usuario}`), e o
 * chamador (`AuthCallbackPage`) a repassa direto a `definirSessao`.
 */
import type { UsuarioSessao } from '@/lib/auth';
import { fetchSSOConfig } from './config';
import { SESSION_KEY_STATE, SESSION_KEY_VERIFIER } from './pkce';

export class ErroCallbackSSO extends Error {
  constructor(
    public codigo: string,
    message?: string,
  ) {
    super(message ?? codigo);
    this.name = 'ErroCallbackSSO';
  }
}

export interface SessaoSSO {
  token: string;
  usuario: UsuarioSessao;
}

export async function trocarCodePorSessao(searchParams: URLSearchParams): Promise<SessaoSSO> {
  const code = searchParams.get('code');
  const state = searchParams.get('state');
  const erroParam = searchParams.get('error');

  if (erroParam) {
    throw new ErroCallbackSSO('KEYCLOAK_ERRO', `Keycloak retornou erro: ${erroParam}`);
  }
  if (!code || !state) {
    throw new ErroCallbackSSO('PARAMS_AUSENTES', 'Callback sem code/state.');
  }

  // Lê e REMOVE verifier+state do sessionStorage antes de qualquer validação —
  // um valor de uma tentativa anterior nunca deve sobreviver a este ponto.
  const stateSalvo = sessionStorage.getItem(SESSION_KEY_STATE);
  const verifier = sessionStorage.getItem(SESSION_KEY_VERIFIER);
  sessionStorage.removeItem(SESSION_KEY_STATE);
  sessionStorage.removeItem(SESSION_KEY_VERIFIER);

  if (!verifier || !stateSalvo || stateSalvo !== state) {
    throw new ErroCallbackSSO('CSRF', 'State inválido (possível CSRF) ou sessão de login expirada.');
  }

  const cfg = await fetchSSOConfig();
  if (!cfg.enabled || !cfg.base_url || !cfg.client_id || !cfg.redirect_uri) {
    throw new ErroCallbackSSO('SSO_NAO_CONFIGURADO', 'SSO não configurado neste servidor.');
  }

  let tokenResp: Response;
  try {
    tokenResp = await fetch(`${cfg.base_url}/protocol/openid-connect/token`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: new URLSearchParams({
        grant_type: 'authorization_code',
        client_id: cfg.client_id,
        redirect_uri: cfg.redirect_uri,
        code,
        code_verifier: verifier,
      }),
    });
  } catch {
    throw new ErroCallbackSSO('TROCA_TOKEN', 'Falha de rede ao trocar o código por token.');
  }
  if (!tokenResp.ok) {
    throw new ErroCallbackSSO('TROCA_TOKEN', 'Falha ao trocar o código por token no Keycloak.');
  }
  const tokenData = (await tokenResp.json()) as { access_token?: string };
  if (!tokenData.access_token) {
    throw new ErroCallbackSSO('TROCA_TOKEN', 'Resposta do Keycloak sem access_token.');
  }

  const res = await fetch('/api/auth/sso/keycloak', {
    method: 'POST',
    headers: { Authorization: `Bearer ${tokenData.access_token}` },
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: { code?: string } };
    // Propaga o `code` do backend para a página distinguir "sem conta" de
    // "e-mail não verificado".
    throw new ErroCallbackSSO(body.error?.code ?? 'SSO_FALHA', 'Falha ao concluir o login via Ferreira Costa.');
  }
  return (await res.json()) as SessaoSSO;
}
