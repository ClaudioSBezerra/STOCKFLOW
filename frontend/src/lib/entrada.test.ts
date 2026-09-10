import { describe, expect, it } from 'vitest';
import { escolherApp, type AppDeEntrada } from './entrada';

describe('escolherApp (Story 9.2)', () => {
  const casos: Array<[string, AppDeEntrada]> = [
    ['/plataforma', 'plataforma'],
    ['/plataforma/', 'plataforma'],
    ['/plataforma/login', 'plataforma'],
    ['/plataformas', 'sem-empresa'],
    ['/e/acme', 'empresa'],
    ['/e/acme/pedidos', 'empresa'],
    ['/e/acme-treinamento/login', 'empresa'],
    ['/', 'sem-empresa'],
    ['/login', 'sem-empresa'],
    ['/e/', 'sem-empresa'],
    ['/e/Acme/login', 'sem-empresa'],
  ];

  it.each(casos)('escolherApp(%j) -> %j', (pathname, esperado) => {
    expect(escolherApp(pathname)).toBe(esperado);
  });
});
