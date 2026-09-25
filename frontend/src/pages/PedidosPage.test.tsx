import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { useEffect } from 'react';
import { cleanup, render, screen } from '@testing-library/react';
import { PedidosPage } from './PedidosPage';
import { PedidosFilaPage } from './PedidosFilaPage';

// useAuth() fornece o papel do ator — configurável por teste.
const authState = vi.hoisted(() => ({ papel: 'usuario' as string }));

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: { id: '1', nome: 'Ator', email: 'ator@empresa.com', papel: authState.papel },
    definirSessao: vi.fn(),
    atualizarUsuario: vi.fn(),
    logout: vi.fn(),
  }),
}));

// MeusPedidosSection/FilaPedidosSection são mockadas por inteiro — esta
// suíte testa só o WIRING de PedidosPage (gate de papel + Tabs), não o
// comportamento interno de cada seção (já coberto por
// MeusPedidosSection.test.tsx e FilaPedidosSection.test.tsx). Cada mock
// registra montagem/desmontagem via os spies abaixo, para provar que a troca
// de aba desmonta/remonta corretamente (molde de Radix Tabs — TabsContent
// desmonta o conteúdo inativo por padrão) sem erro.
const montarMeusMock = vi.hoisted(() => vi.fn());
const desmontarMeusMock = vi.hoisted(() => vi.fn());
const montarFilaMock = vi.hoisted(() => vi.fn());
const desmontarFilaMock = vi.hoisted(() => vi.fn());

vi.mock('@/components/pedidos/MeusPedidosSection', () => ({
  MeusPedidosSection: () => {
    useEffect(() => {
      montarMeusMock();
      return () => desmontarMeusMock();
    }, []);
    return <div data-testid="secao-meus-pedidos">Meus Pedidos (mock)</div>;
  },
}));

vi.mock('@/components/pedidos/FilaPedidosSection', () => ({
  FilaPedidosSection: () => {
    useEffect(() => {
      montarFilaMock();
      return () => desmontarFilaMock();
    }, []);
    return <div data-testid="secao-fila-pedidos">Fila de Pedidos (mock)</div>;
  },
}));

beforeEach(() => {
  authState.papel = 'usuario';
});

afterEach(() => {
  // `cleanup()` explícito ANTES de `clearAllMocks()` (em vez de depender só
  // do afterEach automático de @testing-library/react, cuja ordem de
  // registro relativa ao nosso próprio afterEach não é garantida): sem isso,
  // um componente ainda montado no fim de um teste (ex.: FilaPedidosSection
  // depois de "clica em Fila" no it.each abaixo) podia desmontar DEPOIS de
  // clearAllMocks já ter zerado os spies, vazando uma chamada de
  // desmontarFilaMock para a contagem do próximo teste.
  cleanup();
  vi.clearAllMocks();
});

describe('PedidosPage (/pedidos) — Meus pedidos', () => {
  it.each(['usuario', 'almoxarife', 'gestor', 'adm'])(
    'papel %s vê Meus pedidos direto, sem abas e sem a Fila',
    (papel) => {
      authState.papel = papel;
      render(<PedidosPage />);

      expect(screen.getByTestId('secao-meus-pedidos')).toBeInTheDocument();
      expect(screen.queryByTestId('secao-fila-pedidos')).not.toBeInTheDocument();
      expect(screen.queryByRole('tab')).not.toBeInTheDocument();
      expect(montarFilaMock).not.toHaveBeenCalled();
    },
  );
});

describe('PedidosFilaPage (/pedidos/fila) — gate de papel', () => {
  it.each(['almoxarife', 'gestor', 'adm'])('papel %s cai direto na Fila', (papel) => {
    authState.papel = papel;
    render(<PedidosFilaPage />);

    expect(screen.getByTestId('secao-fila-pedidos')).toBeInTheDocument();
    expect(screen.queryByTestId('secao-meus-pedidos')).not.toBeInTheDocument();
    expect(montarFilaMock).toHaveBeenCalledTimes(1);
  });

  it('papel usuario vê a recusa e a Fila nem é montada (o servidor segue como autoridade)', () => {
    authState.papel = 'usuario';
    render(<PedidosFilaPage />);

    expect(screen.getByText('Você não tem acesso à Fila de aprovação.')).toBeInTheDocument();
    expect(screen.queryByTestId('secao-fila-pedidos')).not.toBeInTheDocument();
    expect(montarFilaMock).not.toHaveBeenCalled();
  });
});
