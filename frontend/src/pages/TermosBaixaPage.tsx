import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  baixarTermoBaixaPDF,
  listarTermosBaixa,
  MENSAGEM_ERRO_LISTAR_TERMOS,
  MENSAGEM_ERRO_TERMO_BAIXA,
  type FiltroTermosBaixa,
  type TermoBaixaResumo,
} from '@/lib/pedidos';
import { MOTIVOS_SAIDA, rotuloCurtoMotivo } from '@/lib/motivosSaida';

/**
 * Página "Termos de Baixa" (`/termos-baixa`, spec-termo-baixa-pendencias) —
 * lista, busca e reimpressão do PDF dos Termos de Baixa. Visível a todos os
 * papéis: o servidor decide o escopo (gestor+ vê todos da Empresa; os demais
 * só os Termos dos próprios Pedidos) e revalida o acesso a cada PDF.
 * Download no molde de BaixarTermoBaixaButton (Blob -> object URL -> `<a download>`).
 */

const TAMANHO_PAGINA = 50;
const ANO_MIN = 2000;
const ANO_MAX = 2100;
const MENSAGEM_PERIODO = 'A data final não pode ser anterior à data inicial.';
const MENSAGEM_ANO = `Use datas entre ${ANO_MIN} e ${ANO_MAX}.`;

function anoValido(v?: string): boolean {
  if (!v) return true;
  const ano = Number(v.slice(0, 4));
  return ano >= ANO_MIN && ano <= ANO_MAX;
}

const FILTRO_VAZIO: FiltroTermosBaixa = { numero: '', motivo: '', de: '', ate: '', q: '' };

export function TermosBaixaPage() {
  const [termos, setTermos] = useState<TermoBaixaResumo[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [erroFiltro, setErroFiltro] = useState<string | null>(null);
  const [carregou, setCarregou] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [rascunho, setRascunho] = useState<FiltroTermosBaixa>(FILTRO_VAZIO);
  const [aplicado, setAplicado] = useState<FiltroTermosBaixa>(FILTRO_VAZIO);
  const [baixandoId, setBaixandoId] = useState<string | null>(null);
  const seqRef = useRef(0);

  const carregar = useCallback(async (filtro: FiltroTermosBaixa, desloc: number) => {
    const seq = ++seqRef.current;
    setErro(null);
    try {
      const lista = await listarTermosBaixa({ ...filtro, limite: String(TAMANHO_PAGINA), offset: String(desloc) });
      if (seq !== seqRef.current) return;
      setTermos(lista.termos);
      setTotal(lista.total);
    } catch (err) {
      if (seq !== seqRef.current) return;
      setErro(err instanceof Error ? err.message : MENSAGEM_ERRO_LISTAR_TERMOS);
    } finally {
      if (seq === seqRef.current) setCarregou(true);
    }
  }, []);

  useEffect(() => {
    void carregar(aplicado, offset);
  }, [carregar, aplicado, offset]);

  function aoFiltrar(event: FormEvent) {
    event.preventDefault();
    if (rascunho.de && rascunho.ate && rascunho.ate < rascunho.de) {
      setErroFiltro(MENSAGEM_PERIODO);
      return;
    }
    if (!anoValido(rascunho.de) || !anoValido(rascunho.ate)) {
      setErroFiltro(MENSAGEM_ANO);
      return;
    }
    setErroFiltro(null);
    setCarregou(false);
    setOffset(0);
    setAplicado({ ...rascunho });
  }

  function aoLimpar() {
    setRascunho(FILTRO_VAZIO);
    setErroFiltro(null);
    setCarregou(false);
    setOffset(0);
    setAplicado(FILTRO_VAZIO);
  }

  function irParaPagina(novoOffset: number) {
    setCarregou(false);
    setOffset(Math.max(0, novoOffset));
  }

  async function aoBaixar(termo: TermoBaixaResumo) {
    setBaixandoId(termo.id);
    try {
      const blob = await baixarTermoBaixaPDF(termo.id);
      const href = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = href;
      link.download = `termo-baixa-${termo.numero}.pdf`;
      document.body.appendChild(link);
      link.click();
      link.remove();
      // Revogar no mesmo tick do click() pode cancelar o download no Firefox/Safari.
      setTimeout(() => URL.revokeObjectURL(href), 0);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : MENSAGEM_ERRO_TERMO_BAIXA);
    } finally {
      setBaixandoId(null);
    }
  }

  const filtrando = Object.values(aplicado).some((v) => (v ?? '').trim() !== '');
  const vazio = carregou && !erro && termos.length === 0;

  return (
    <Card>
      <CardHeader>
        <h1 className="text-heading-lg">Termos de Baixa</h1>
        <CardDescription>
          Consulte e baixe novamente o PDF dos Termos de Baixa. Gestores veem todos os Termos da
          Empresa; os demais usuários veem os Termos dos seus próprios pedidos.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form onSubmit={aoFiltrar} className="flex flex-wrap items-end gap-3" aria-label="Filtros">
          <div className="flex flex-col gap-1">
            <Label htmlFor="termos-numero">Número</Label>
            <Input
              id="termos-numero"
              type="number"
              min={1}
              inputMode="numeric"
              className="w-28"
              value={rascunho.numero ?? ''}
              onChange={(e) => setRascunho({ ...rascunho, numero: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="termos-motivo">Motivo</Label>
            <select
              id="termos-motivo"
              value={rascunho.motivo ?? ''}
              onChange={(e) => setRascunho({ ...rascunho, motivo: e.target.value })}
              className="text-body h-9 rounded-md border border-border bg-background px-3"
            >
              <option value="">Todos</option>
              {MOTIVOS_SAIDA.map((m) => (
                <option key={m.valor} value={m.valor}>
                  {m.curto}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="termos-de">De</Label>
            <Input
              id="termos-de"
              type="date"
              value={rascunho.de ?? ''}
              onChange={(e) => setRascunho({ ...rascunho, de: e.target.value })}
            />
          </div>
          <div className="flex flex-col gap-1">
            <Label htmlFor="termos-ate">Até</Label>
            <Input
              id="termos-ate"
              type="date"
              value={rascunho.ate ?? ''}
              onChange={(e) => setRascunho({ ...rascunho, ate: e.target.value })}
            />
          </div>
          <div className="flex min-w-48 flex-1 flex-col gap-1">
            <Label htmlFor="termos-q">Solicitante ou aprovador</Label>
            <Input
              id="termos-q"
              value={rascunho.q ?? ''}
              onChange={(e) => setRascunho({ ...rascunho, q: e.target.value })}
            />
          </div>
          <Button type="submit">Filtrar</Button>
          <Button type="button" variant="outline" onClick={aoLimpar}>
            Limpar
          </Button>
        </form>

        {erroFiltro && (
          <p role="alert" className="text-body text-destructive">
            {erroFiltro}
          </p>
        )}

        {erro && (
          <div role="alert" className="flex flex-wrap items-center gap-3 text-body text-destructive">
            <span>{erro}</span>
            <Button type="button" variant="outline" size="sm" onClick={() => void carregar(aplicado, offset)}>
              Tentar de novo
            </Button>
          </div>
        )}

        {!carregou && !erro && (
          <output className="text-body text-muted-foreground">Carregando Termos de Baixa...</output>
        )}

        {vazio && (
          <p className="text-body text-muted-foreground">
            {filtrando
              ? 'Nenhum Termo de Baixa encontrado para esses filtros.'
              : 'Nenhum Termo de Baixa por aqui ainda.'}
          </p>
        )}

        {termos.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-body">
              <thead>
                <tr className="text-label text-muted-foreground">
                  <th className="py-2 pr-4 text-left font-medium">Número</th>
                  <th className="py-2 pr-4 text-left font-medium">Motivo</th>
                  <th className="py-2 pr-4 text-left font-medium">Data</th>
                  <th className="py-2 pr-4 text-left font-medium">Aprovador</th>
                  <th className="py-2 pr-4 text-left font-medium">Solicitantes</th>
                  <th className="py-2 pr-4 text-left font-medium">Itens</th>
                  <th className="py-2 text-left font-medium">
                    <span className="sr-only">Ações</span>
                  </th>
                </tr>
              </thead>
              <tbody>
                {termos.map((t) => (
                  <tr key={t.id} className="border-t border-border">
                    <td className="py-2 pr-4 tabular-nums">{t.numero}</td>
                    <td className="py-2 pr-4">
                      <span className="text-label inline-flex rounded-full bg-info/10 px-2 py-0.5 font-medium text-[color:var(--color-text-on-tint-info)]">
                        {rotuloCurtoMotivo(t.motivoSaida)}
                      </span>
                    </td>
                    <td className="py-2 pr-4">{new Date(t.criadoEm).toLocaleString('pt-BR')}</td>
                    <td className="py-2 pr-4">{t.aprovador || '—'}</td>
                    <td className="py-2 pr-4 max-w-xs break-words">
                      {t.solicitantes.length > 0 ? t.solicitantes.join(', ') : '—'}
                    </td>
                    <td className="py-2 pr-4 tabular-nums">{t.quantidadeItens}</td>
                    <td className="py-2">
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => void aoBaixar(t)}
                        disabled={baixandoId === t.id}
                        aria-busy={baixandoId === t.id}
                        aria-label={`Baixar PDF do Termo de Baixa nº ${t.numero}`}
                      >
                        {baixandoId === t.id ? 'Baixando...' : 'Baixar PDF'}
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}

        {total > TAMANHO_PAGINA && (
          <nav aria-label="Paginação" className="flex flex-wrap items-center gap-3">
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={offset === 0 || !carregou}
              onClick={() => irParaPagina(offset - TAMANHO_PAGINA)}
            >
              Anterior
            </Button>
            <output aria-live="polite" className="text-label text-muted-foreground">
              {offset + 1}–{Math.min(offset + TAMANHO_PAGINA, total)} de {total}
            </output>
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={offset + TAMANHO_PAGINA >= total || !carregou}
              onClick={() => irParaPagina(offset + TAMANHO_PAGINA)}
            >
              Próxima
            </Button>
          </nav>
        )}
      </CardContent>
    </Card>
  );
}

export default TermosBaixaPage;
