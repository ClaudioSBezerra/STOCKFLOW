import { describe, expect, it } from 'vitest';
import type { RouteObject } from 'react-router-dom';
import { isValidElement } from 'react';
import { router } from './App';
import { EmpresaPage } from '@/pages/cadastros/EmpresaPage';

function todas(rotas: RouteObject[]): RouteObject[] {
  return rotas.flatMap((r) => [r, ...todas(r.children ?? [])]);
}

describe('rotas do router real (App.tsx)', () => {
  it('cadastros/empresa renderiza EmpresaPage', () => {
    const rota = todas(router.routes as RouteObject[]).find((r) => r.path === 'cadastros/empresa');
    expect(rota).toBeDefined();
    expect(isValidElement(rota?.element) && rota.element.type).toBe(EmpresaPage);
  });
});
