import { afterEach, describe, expect, it } from 'vitest';
import { clearAccessToken, getAccessToken, setAccessToken } from './session';

describe('session', () => {
  afterEach(() => {
    clearAccessToken();
  });

  it('getAccessToken começa nulo', () => {
    expect(getAccessToken()).toBeNull();
  });

  it('setAccessToken guarda o token e getAccessToken devolve o mesmo valor', () => {
    setAccessToken('token-de-teste');
    expect(getAccessToken()).toBe('token-de-teste');
  });

  it('clearAccessToken limpa o token guardado', () => {
    setAccessToken('token-de-teste');
    clearAccessToken();
    expect(getAccessToken()).toBeNull();
  });

  it('setAccessToken sobrescreve um token anterior', () => {
    setAccessToken('primeiro');
    setAccessToken('segundo');
    expect(getAccessToken()).toBe('segundo');
  });
});
