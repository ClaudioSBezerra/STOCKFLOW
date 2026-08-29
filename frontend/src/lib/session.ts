/**
 * Guarda do access token de sessão (Story 1.4, spec-1-4, AD-6). Só em
 * memória — uma variável de módulo — NUNCA `localStorage`/`sessionStorage`:
 * o access token de 30min não sobrevive a um reload da página por design;
 * quem precisa de persistência entre abas/reloads é o refresh token, que já
 * vive em cookie HttpOnly (fora do alcance de JavaScript).
 *
 * Nenhum bootstrap automático de sessão existe ainda (silent refresh via
 * cookie ao montar `App.tsx`) — deliberadamente fora do escopo desta story,
 * já que `AppShell` não gateia nada por papel até a Story 1.5.
 */
let accessToken: string | null = null;

export function getAccessToken(): string | null {
  return accessToken;
}

export function setAccessToken(token: string): void {
  accessToken = token;
}

export function clearAccessToken(): void {
  accessToken = null;
}
