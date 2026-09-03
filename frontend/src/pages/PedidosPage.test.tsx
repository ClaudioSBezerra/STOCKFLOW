import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { useEffect } from 'react';
import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { PedidosPage } from './PedidosPage';

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

describe('PedidosPage — gate de papel', () => {
  it('papel usuario vê só a aba "Meus Pedidos" — "Fila" não existe na tela', () => {
    authState.papel = 'usuario';
    render(<PedidosPage />);

    expect(screen.getByRole('tab', { name: 'Meus Pedidos' })).toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Fila' })).not.toBeInTheDocument();
    expect(screen.getByTestId('secao-meus-pedidos')).toBeInTheDocument();
    expect(screen.queryByTestId('secao-fila-pedidos')).not.toBeInTheDocument();
  });

  it.each(['almoxarife', 'gestor', 'adm'])(
    'papel %s vê as abas "Meus Pedidos" e "Fila" e pode alternar entre elas',
    async (papel) => {
      authState.papel = papel;
      render(<PedidosPage />);

      expect(screen.getByRole('tab', { name: 'Meus Pedidos' })).toBeInTheDocument();
      expect(screen.getByRole('tab', { name: 'Fila' })).toBeInTheDocument();
      // Aba default: "Meus Pedidos".
      expect(screen.getByTestId('secao-meus-pedidos')).toBeInTheDocument();
      expect(screen.queryByTestId('secao-fila-pedidos')).not.toBeInTheDocument();

      const user = userEvent.setup();
      await user.click(screen.getByRole('tab', { name: 'Fila' }));

      expect(screen.getByRole('tab', { name: 'Fila', selected: true })).toBeInTheDocument();
      expect(screen.getByTestId('secao-fila-pedidos')).toBeInTheDocument();
    },
  );

  it('trocar de "Meus Pedidos" para "Fila" desmonta/remonta cada seção corretamente, sem erro', async () => {
    authState.papel = 'almoxarife';
    render(<PedidosPage />);

    expect(montarMeusMock).toHaveBeenCalledTimes(1);
    expect(montarFilaMock).not.toHaveBeenCalled();

    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: 'Fila' }));

    expect(montarFilaMock).toHaveBeenCalledTimes(1);
    expect(desmontarMeusMock).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('secao-fila-pedidos')).toBeInTheDocument();
    expect(screen.queryByTestId('secao-meus-pedidos')).not.toBeInTheDocument();

    await user.click(screen.getByRole('tab', { name: 'Meus Pedidos' }));

    expect(montarMeusMock).toHaveBeenCalledTimes(2);
    expect(desmontarFilaMock).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('secao-meus-pedidos')).toBeInTheDocument();
    expect(screen.queryByTestId('secao-fila-pedidos')).not.toBeInTheDocument();
  });

  // Regressão de code review: `Tabs` é não-controlado (`defaultValue`) — sem
  // a `key={String(podeVerFila)}` em PedidosPage.tsx, uma rebaixada de papel
  // refletida em tempo real (ex.: AuthProvider atualiza `usuario.papel` sem
  // reload de página) enquanto "Fila" era a aba ativa faria o
  // TabsTrigger/TabsContent daquela aba sumirem do JSX, mas o estado interno
  // do Radix continuaria apontando para "fila" — nenhum painel visível até
  // um clique manual em "Meus Pedidos". Este teste prova que a remontagem
  // forçada pela `key` evita esse painel em branco.
  it('rebaixar o papel com "Fila" ativa não deixa um painel em branco — volta para "Meus Pedidos"', async () => {
    authState.papel = 'almoxarife';
    const { rerender } = render(<PedidosPage />);

    const user = userEvent.setup();
    await user.click(screen.getByRole('tab', { name: 'Fila' }));
    expect(screen.getByTestId('secao-fila-pedidos')).toBeInTheDocument();

    // Rebaixamento de papel refletido em tempo real, sem reload de página —
    // simulado por um re-render com o mesmo authState mutado (mesmo molde de
    // "papel muda sob o mesmo componente montado").
    authState.papel = 'usuario';
    rerender(<PedidosPage />);

    // Nenhum painel em branco: "Meus Pedidos" volta a ser o conteúdo
    // visível, e a aba "Fila" (inacessível para o novo papel) some.
    expect(screen.getByTestId('secao-meus-pedidos')).toBeInTheDocument();
    expect(screen.queryByTestId('secao-fila-pedidos')).not.toBeInTheDocument();
    expect(screen.queryByRole('tab', { name: 'Fila' })).not.toBeInTheDocument();
  });
});
