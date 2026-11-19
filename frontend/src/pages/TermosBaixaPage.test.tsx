import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { TermosBaixaPage } from './TermosBaixaPage';

const listarMock = vi.hoisted(() => vi.fn());
const baixarMock = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());
vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: toastError, info: vi.fn() } }));
vi.mock('@/lib/pedidos', () => ({
  listarTermosBaixa: listarMock,
  baixarTermoBaixaPDF: baixarMock,
  MENSAGEM_ERRO_LISTAR_TERMOS: 'Não foi possível carregar os Termos de Baixa agora. Tente novamente em instantes.',
  MENSAGEM_ERRO_TERMO_BAIXA: 'Não foi possível baixar o Termo de Baixa agora. Tente novamente em instantes.',
}));

const TERMOS = [
  {
    id: 't-2',
    numero: 2,
    motivoSaida: 'vencido_avariado',
    criadoEm: '2026-11-19T12:00:00Z',
    aprovador: 'Gestor Ávila',
    solicitantes: ['Ana', 'Bruno'],
    quantidadeItens: 3,
  },
  {
    id: 't-1',
    numero: 1,
    motivoSaida: null,
    criadoEm: '2026-11-18T12:00:00Z',
    aprovador: 'Gestor Ávila',
    solicitantes: ['Ana'],
    quantidadeItens: 1,
  },
];

describe('TermosBaixaPage', () => {
  beforeEach(() => {
    listarMock.mockReset();
    baixarMock.mockReset();
    toastError.mockReset();
    URL.createObjectURL = vi.fn(() => 'blob:x');
    URL.revokeObjectURL = vi.fn();
  });
  afterEach(() => vi.restoreAllMocks());

  it('lista os Termos com motivo, aprovador, solicitantes e itens', async () => {
    listarMock.mockResolvedValue({ termos: TERMOS, total: TERMOS.length });
    render(<TermosBaixaPage />);
    expect(await screen.findByText('Vencido ou avariado')).toBeInTheDocument();
    expect(screen.getByText('Ana, Bruno')).toBeInTheDocument();
    expect(screen.getByText('Motivo não informado')).toBeInTheDocument();
    expect(listarMock).toHaveBeenCalledWith({ numero: '', motivo: '', de: '', ate: '', q: '', limite: '50', offset: '0' });
  });

  it('estado vazio sem filtro e com filtro', async () => {
    listarMock.mockResolvedValue({ termos: [], total: 0 });
    render(<TermosBaixaPage />);
    expect(await screen.findByText('Nenhum Termo de Baixa por aqui ainda.')).toBeInTheDocument();
    await userEvent.type(screen.getByLabelText('Número'), '9');
    await userEvent.click(screen.getByRole('button', { name: 'Filtrar' }));
    expect(await screen.findByText('Nenhum Termo de Baixa encontrado para esses filtros.')).toBeInTheDocument();
    expect(listarMock).toHaveBeenLastCalledWith(expect.objectContaining({ numero: '9' }));
  });

  it('filtros são enviados ao Filtrar e Limpar volta ao padrão', async () => {
    listarMock.mockResolvedValue({ termos: TERMOS, total: TERMOS.length });
    render(<TermosBaixaPage />);
    await screen.findByText('Ana, Bruno');
    await userEvent.selectOptions(screen.getByLabelText('Motivo'), 'perda_roubo');
    await userEvent.type(screen.getByLabelText('Solicitante ou aprovador'), 'ana');
    await userEvent.click(screen.getByRole('button', { name: 'Filtrar' }));
    await waitFor(() =>
      expect(listarMock).toHaveBeenLastCalledWith(expect.objectContaining({ motivo: 'perda_roubo', q: 'ana' })),
    );
    await userEvent.click(screen.getByRole('button', { name: 'Limpar' }));
    await waitFor(() => expect(listarMock).toHaveBeenLastCalledWith(expect.objectContaining({ motivo: '', q: '' })));
  });

  it('erro mostra alerta e "Tentar de novo" recarrega', async () => {
    listarMock.mockRejectedValueOnce(new Error('falhou geral'));
    listarMock.mockResolvedValueOnce({ termos: TERMOS, total: TERMOS.length });
    render(<TermosBaixaPage />);
    expect(await screen.findByRole('alert')).toHaveTextContent('falhou geral');
    await userEvent.click(screen.getByRole('button', { name: 'Tentar de novo' }));
    expect(await screen.findByText('Ana, Bruno')).toBeInTheDocument();
  });

  it('Baixar PDF baixa termo-baixa-N.pdf; falha vira toast', async () => {
    listarMock.mockResolvedValue({ termos: TERMOS, total: TERMOS.length });
    baixarMock.mockResolvedValueOnce(new Blob(['%PDF-']));
    const clique = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    render(<TermosBaixaPage />);
    await userEvent.click(await screen.findByRole('button', { name: 'Baixar PDF do Termo de Baixa nº 2' }));
    await waitFor(() => expect(clique).toHaveBeenCalled());
    expect(baixarMock).toHaveBeenCalledWith('t-2');
    expect((clique.mock.contexts[0] as HTMLAnchorElement).download).toBe('termo-baixa-2.pdf');

    baixarMock.mockRejectedValueOnce(new Error('sem acesso'));
    await userEvent.click(screen.getByRole('button', { name: 'Baixar PDF do Termo de Baixa nº 1' }));
    await waitFor(() => expect(toastError).toHaveBeenCalledWith('sem acesso'));
  });

  it('formata data e número, e a pílula de motivo null mostra "Motivo não informado"', async () => {
    listarMock.mockResolvedValue({ termos: TERMOS, total: 2 });
    render(<TermosBaixaPage />);
    const linha1 = (await screen.findByText('Motivo não informado')).closest('tr') as HTMLElement;
    expect(linha1).toHaveTextContent(new Date('2026-11-18T12:00:00Z').toLocaleString('pt-BR'));
    expect(linha1.querySelectorAll('td')[0]).toHaveTextContent('1');
    expect(linha1.querySelectorAll('td')[5]).toHaveTextContent('1');
    const linha2 = screen.getAllByRole('row')[1];
    expect(linha2).toHaveTextContent(new Date('2026-11-19T12:00:00Z').toLocaleString('pt-BR'));
    expect(linha2.querySelectorAll('td')[0]).toHaveTextContent('2');
  });

  it('filtro de período preenchido é enviado ao servidor', async () => {
    listarMock.mockResolvedValue({ termos: TERMOS, total: 2 });
    render(<TermosBaixaPage />);
    await screen.findByText('Ana, Bruno');
    await userEvent.type(screen.getByLabelText('De'), '2026-11-01');
    await userEvent.type(screen.getByLabelText('Até'), '2026-11-19');
    await userEvent.click(screen.getByRole('button', { name: 'Filtrar' }));
    await waitFor(() =>
      expect(listarMock).toHaveBeenLastCalledWith(expect.objectContaining({ de: '2026-11-01', ate: '2026-11-19', offset: '0' })),
    );
  });

  it('data final anterior à inicial mostra erro e não chama o servidor', async () => {
    listarMock.mockResolvedValue({ termos: TERMOS, total: 2 });
    render(<TermosBaixaPage />);
    await screen.findByText('Ana, Bruno');
    const chamadas = listarMock.mock.calls.length;
    await userEvent.type(screen.getByLabelText('De'), '2026-11-19');
    await userEvent.type(screen.getByLabelText('Até'), '2026-11-01');
    await userEvent.click(screen.getByRole('button', { name: 'Filtrar' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('A data final não pode ser anterior à data inicial.');
    expect(listarMock).toHaveBeenCalledTimes(chamadas);
  });

  it('ano fora de 2000..2100 mostra erro', async () => {
    listarMock.mockResolvedValue({ termos: TERMOS, total: 2 });
    render(<TermosBaixaPage />);
    await screen.findByText('Ana, Bruno');
    await userEvent.type(screen.getByLabelText('De'), '1999-01-01');
    await userEvent.click(screen.getByRole('button', { name: 'Filtrar' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Use datas entre 2000 e 2100.');
  });

  it('com mais de 50 Termos mostra Anterior/Próxima e alcança a página seguinte', async () => {
    listarMock.mockResolvedValue({ termos: TERMOS, total: 120 });
    render(<TermosBaixaPage />);
    await screen.findByText('Ana, Bruno');
    expect(screen.getByText('1–50 de 120')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Anterior' })).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: 'Próxima' }));
    await waitFor(() => expect(listarMock).toHaveBeenLastCalledWith(expect.objectContaining({ limite: '50', offset: '50' })));
    expect(await screen.findByText('51–100 de 120')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Próxima' }));
    expect(await screen.findByText('101–120 de 120')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Próxima' })).toBeDisabled();
    await userEvent.click(screen.getByRole('button', { name: 'Anterior' }));
    await waitFor(() => expect(listarMock).toHaveBeenLastCalledWith(expect.objectContaining({ offset: '50' })));
  });

  it('até 50 Termos não mostra paginação', async () => {
    listarMock.mockResolvedValue({ termos: TERMOS, total: 2 });
    render(<TermosBaixaPage />);
    await screen.findByText('Ana, Bruno');
    expect(screen.queryByRole('navigation', { name: 'Paginação' })).not.toBeInTheDocument();
  });
});
