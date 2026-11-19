import { describe, expect, it } from 'vitest';
import type { RouteObject } from 'react-router-dom';
import { isValidElement } from 'react';
import { Navigate } from 'react-router-dom';
import { router } from './App';
import { PedidoOutrasSaidasPage } from '@/pages/PedidoOutrasSaidasPage';
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

  it('pedidos/outras-saidas renderiza PedidoOutrasSaidasPage e pedidos/baixa redireciona para ela', () => {
    const rotas = todas(router.routes as RouteObject[]);
    const nova = rotas.find((r) => r.path === 'pedidos/outras-saidas');
    expect(isValidElement(nova?.element) && nova.element.type).toBe(PedidoOutrasSaidasPage);
    const antiga = rotas.find((r) => r.path === 'pedidos/baixa');
    expect(isValidElement(antiga?.element) && antiga.element.type).toBe(Navigate);
    expect(isValidElement(antiga?.element) && (antiga.element.props as { to: string }).to).toBe(
      '/pedidos/outras-saidas',
    );
  });
});
