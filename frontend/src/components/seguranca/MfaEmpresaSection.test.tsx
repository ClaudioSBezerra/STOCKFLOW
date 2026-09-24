import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MfaEmpresaSection } from './MfaEmpresaSection';

const authState = vi.hoisted(() => ({
  papel: 'adm' as string,
  mfaHabilitado: true,
  origem: 'senha' as string,
  empresaMfaObrigatorio: false,
}));
const atualizarUsuarioMock = vi.hoisted(() => vi.fn());

vi.mock('@/lib/auth', () => ({
  useAuth: () => ({
    estado: 'autenticado',
    usuario: {
      id: 'adm-1',
      nome: 'Adm',
      email: 'adm@empresa.com',
      papel: authState.papel,
      mfaHabilitado: authState.mfaHabilitado,
      origem: authState.origem,
      empresa: { mfaObrigatorio: authState.empresaMfaObrigatorio },
    },
    definirSessao: vi.fn(),
    atualizarUsuario: atualizarUsuarioMock,
    logout: vi.fn(),
  }),
}));

const toastMock = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock('sonner', () => ({ toast: toastMock }));

const obterMock = vi.hoisted(() => vi.fn());
const alterarMock = vi.hoisted(() => vi.fn());
const listarMock = vi.hoisted(() => vi.fn());
vi.mock('@/lib/segurancaEmpresa', async () => {
  const actual = await vi.importActual<typeof import('@/lib/segurancaEmpresa')>(
    '@/lib/segurancaEmpresa',
  );
  return {
    ...actual,
    obterExigenciaMFA: obterMock,
    alterarExigenciaMFA: alterarMock,
    listarAuditoriaSeguranca: listarMock,
  };
});

const EVENTO = {
  id: 'e1',
  acao: 'exigencia_alterada',
  atorId: 'adm-1',
  atorNome: 'Ana Administradora',
  alvoId: null,
  alvoNome: null,
  detalhe: { anterior: false, novo: true },
  criadoEm: '2026-09-24T10:00:00Z',
};

beforeEach(() => {
  authState.papel = 'adm';
  authState.mfaHabilitado = true;
  authState.origem = 'senha';
  authState.empresaMfaObrigatorio = false;
  obterMock.mockReset();
  alterarMock.mockReset();
  listarMock.mockReset();
  atualizarUsuarioMock.mockReset();
  listarMock.mockResolvedValue([]);
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('MfaEmpresaSection', () => {
  it('não renderiza nada abaixo de adm e não chama a API', () => {
    authState.papel = 'gestor';
    const { container } = render(<MfaEmpresaSection />);
    expect(container).toBeEmptyDOMElement();
    expect(obterMock).not.toHaveBeenCalled();
    expect(listarMock).not.toHaveBeenCalled();
  });

  it('exibe o estado atual', async () => {
    obterMock.mockResolvedValue({ mfaObrigatorio: true, contasSemMfa: 0 });
    render(<MfaEmpresaSection />);

    expect(await screen.findByText('Exigida')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Deixar de exigir' })).toBeInTheDocument();
  });

  it('ligar abre a confirmação com a contagem, e cancelar não faz o PUT', async () => {
    obterMock.mockResolvedValue({ mfaObrigatorio: false, contasSemMfa: 3 });
    const user = userEvent.setup();
    render(<MfaEmpresaSection />);

    await user.click(await screen.findByRole('button', { name: 'Passar a exigir' }));
    const dialogo = await screen.findByRole('alertdialog');
    expect(dialogo).toHaveTextContent(
      '3 conta(s) gestor/adm ainda sem dupla autenticação ficarão sem acesso até cadastrar.',
    );
    expect(dialogo).not.toHaveTextContent('Isso inclui você.');

    await user.click(screen.getByRole('button', { name: 'Cancelar' }));
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    expect(alterarMock).not.toHaveBeenCalled();
  });

  it('avisa "Isso inclui você." quando o próprio adm não tem MFA', async () => {
    authState.mfaHabilitado = false;
    obterMock.mockResolvedValue({ mfaObrigatorio: false, contasSemMfa: 1 });
    const user = userEvent.setup();
    render(<MfaEmpresaSection />);

    await user.click(await screen.findByRole('button', { name: 'Passar a exigir' }));
    expect(await screen.findByRole('alertdialog')).toHaveTextContent('Isso inclui você.');
  });

  it('não avisa "Isso inclui você." para sessão SSO, que o gate nunca bloqueia', async () => {
    authState.mfaHabilitado = false;
    authState.origem = 'sso';
    obterMock.mockResolvedValue({ mfaObrigatorio: false, contasSemMfa: 1 });
    const user = userEvent.setup();
    render(<MfaEmpresaSection />);

    await user.click(await screen.findByRole('button', { name: 'Passar a exigir' }));
    const dialogo = await screen.findByRole('alertdialog');
    expect(dialogo).toHaveTextContent('1 conta(s)');
    expect(dialogo).not.toHaveTextContent('Isso inclui você.');
  });

  it('falha do PUT mostra o erro e não altera estado, sessão nem histórico', async () => {
    authState.empresaMfaObrigatorio = true;
    obterMock.mockResolvedValue({ mfaObrigatorio: true, contasSemMfa: 0 });
    alterarMock.mockRejectedValue(new Error('configure o MFA'));
    const user = userEvent.setup();
    render(<MfaEmpresaSection />);

    await user.click(await screen.findByRole('button', { name: 'Deixar de exigir' }));

    await waitFor(() => expect(toastMock.error).toHaveBeenCalledWith('configure o MFA'));
    expect(atualizarUsuarioMock).not.toHaveBeenCalled();
    expect(toastMock.success).not.toHaveBeenCalled();
    expect(listarMock).toHaveBeenCalledTimes(1);
    expect(screen.getByText('Exigida')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Deixar de exigir' })).toBeEnabled();
  });

  it('confirmar faz o PUT com true, atualiza a sessão e recarrega o histórico', async () => {
    authState.mfaHabilitado = false;
    obterMock.mockResolvedValue({ mfaObrigatorio: false, contasSemMfa: 2 });
    alterarMock.mockResolvedValue({ mfaObrigatorio: true, alterado: true });
    const user = userEvent.setup();
    render(<MfaEmpresaSection />);

    await user.click(await screen.findByRole('button', { name: 'Passar a exigir' }));
    await screen.findByRole('alertdialog');
    listarMock.mockResolvedValue([EVENTO]);
    obterMock.mockResolvedValue({ mfaObrigatorio: true, contasSemMfa: 2 });
    await user.click(screen.getByRole('button', { name: 'Confirmar' }));

    await waitFor(() => expect(alterarMock).toHaveBeenCalledWith(true));
    await waitFor(() =>
      expect(atualizarUsuarioMock).toHaveBeenCalledWith(
        expect.objectContaining({ id: 'adm-1', empresa: { mfaObrigatorio: true } }),
      ),
    );
    expect(toastMock.success).toHaveBeenCalled();
    expect(await screen.findByText('Não exigida → Exigida')).toBeInTheDocument();
    expect(listarMock).toHaveBeenCalledTimes(2);
    // A contagem de contas sem MFA é recarregada depois da alteração.
    await waitFor(() => expect(obterMock).toHaveBeenCalledTimes(2));
    expect(await screen.findByRole('button', { name: 'Deixar de exigir' })).toBeInTheDocument();
  });

  it('desligar faz o PUT sem diálogo', async () => {
    authState.empresaMfaObrigatorio = true;
    obterMock.mockResolvedValue({ mfaObrigatorio: true, contasSemMfa: 0 });
    alterarMock.mockResolvedValue({ mfaObrigatorio: false, alterado: true });
    const user = userEvent.setup();
    render(<MfaEmpresaSection />);

    await user.click(await screen.findByRole('button', { name: 'Deixar de exigir' }));

    await waitFor(() => expect(alterarMock).toHaveBeenCalledWith(false));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    await waitFor(() =>
      expect(atualizarUsuarioMock).toHaveBeenCalledWith(
        expect.objectContaining({ empresa: { mfaObrigatorio: false } }),
      ),
    );
  });

  it('renderiza o histórico', async () => {
    obterMock.mockResolvedValue({ mfaObrigatorio: true, contasSemMfa: 0 });
    listarMock.mockResolvedValue([
      { ...EVENTO, id: 'e2', detalhe: { anterior: true, novo: false }, criadoEm: '2026-09-24T11:00:00Z' },
      EVENTO,
    ]);
    render(<MfaEmpresaSection />);

    expect(await screen.findByText('Exigida → Não exigida')).toBeInTheDocument();
    expect(screen.getByText('Não exigida → Exigida')).toBeInTheDocument();
    expect(screen.getAllByText('Ana Administradora')).toHaveLength(2);
  });

  it('descreve mfa_desligado e mfa_resetado pela conta afetada', async () => {
    obterMock.mockResolvedValue({ mfaObrigatorio: false, contasSemMfa: 0 });
    listarMock.mockResolvedValue([
      { ...EVENTO, id: 'e3', acao: 'mfa_desligado', alvoId: 'u1', alvoNome: 'Gil', detalhe: {} },
      { ...EVENTO, id: 'e4', acao: 'mfa_resetado', alvoId: 'u2', alvoNome: 'Rui', detalhe: {} },
    ]);
    render(<MfaEmpresaSection />);

    expect(await screen.findByText('MFA desligado de Gil')).toBeInTheDocument();
    expect(screen.getByText('MFA resetado de Rui')).toBeInTheDocument();
  });

  it('erro de carga vira alerta', async () => {
    obterMock.mockRejectedValue(new Error('x'));
    listarMock.mockRejectedValue(new Error('y'));
    render(<MfaEmpresaSection />);

    const alertas = await screen.findAllByRole('alert');
    expect(alertas.length).toBe(2);
  });
});
