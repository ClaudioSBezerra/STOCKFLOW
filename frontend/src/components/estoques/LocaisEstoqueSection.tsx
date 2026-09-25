import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { MapPin } from 'lucide-react';
import { FaixaIndicadores } from '@/components/lista/FaixaIndicadores';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Seção "Locais" da página `/estoques` (Stories 2.1 e 2.2; padrão de lista na
 * 17.5, sem `Card`): título, faixa de indicadores (`/api/estoques/indicadores`,
 * "—" se falhar; `seqIndRef` descarta respostas fora de ordem), formulário
 * `<Input>` + `<Button>` "Adicionar estoque" que faz `POST /api/estoques`, e a
 * lista de Estoques de `GET /api/estoques` (linhas de 60px com `totalItens`),
 * cada uma com um botão "Excluir".
 *
 * Sucesso do cadastro: `toast.success('Estoque criado.')`, limpa o input e
 * refaz o `GET`. `409` -> `<p role="alert">` específico ("Já existe um estoque
 * com esse nome."). Qualquer outro erro de cadastro, ou falha de carga da
 * lista -> `<p role="alert" className="text-body text-destructive">` genérico
 * (molde de `GestaoUsuariosSection`). Lista vazia -> "Nenhum estoque
 * cadastrado ainda." Botão desabilitado enquanto `enviando` ou o nome está em
 * branco (defesa contra duplo-submit).
 *
 * Exclusão (Story 2.2): "Excluir" numa linha abre o `ConfirmDialog`
 * reutilizável (nunca `window.confirm`); ao confirmar, `DELETE
 * /api/estoques/{id}` com `authHeaders()`. `204`/`res.ok` ->
 * `toast.success('Estoque excluído.')`; `409` (guard de quantidade residual,
 * Story 3.1: o Estoque tem Produto com resíduo) -> `<p role="alert">` com a
 * mensagem do PRÓPRIO servidor (já cita os Produtos, ex. "estoque possui
 * quantidade residual de: Tubo PVC 100mm") — mesma ideia do `409` de nome
 * duplicado no cadastro logo acima, mas aqui a mensagem não é fixa no
 * cliente porque ela varia por Estoque; qualquer outro `!res.ok` ->
 * `MENSAGEM_ERRO_EXCLUIR` genérico no `<p role="alert">`. A lista é sempre
 * recarregada (sucesso E falha, molde de `GestaoUsuariosSection` — a linha
 * obsoleta cai sozinha após um 404 de corrida). O guard de Pedido pendente
 * (Epic 7) entra na Story 7.2, no backend.
 *
 * Story 12.1 (FR-51): todo Estoque novo exige Filial. O formulário ganha um
 * `<select>` obrigatório de Filial (`GET /api/filiais`), o `POST` envia
 * `{ nome, filial_id }` e cada linha mostra a Filial (Estoque legado sem
 * Filial: "—"). Com uma única Filial, ela já vem selecionada.
 *
 * Sem eventos SSE nesta story (o registry `realtime/` ainda não existe): a
 * tela só busca no mount.
 */

interface Estoque {
  id: string;
  nome: string;
  filial_id?: string | null;
  filial_nome?: string | null;
  totalItens?: number;
}

interface IndicadoresLocais {
  locais: number;
  itensEmEstoque: number;
}

interface Filial {
  id: string;
  nome: string;
}

const MENSAGEM_ERRO_CARREGAR =
  'Não foi possível carregar a lista de estoques. Recarregue a página.';
const MENSAGEM_ERRO_CARREGAR_FILIAIS = 'Não foi possível carregar as filiais. Recarregue a página.';
const MENSAGEM_SEM_FILIAIS =
  'Nenhuma filial cadastrada. Peça a um administrador para cadastrar uma filial antes de criar estoques.';
const MENSAGEM_ERRO_CADASTRO =
  'Não foi possível cadastrar o estoque agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_DUPLICADO = 'Já existe um estoque com esse nome.';
const MENSAGEM_ERRO_EXCLUIR =
  'Não foi possível excluir o estoque agora. Tente novamente em instantes.';

export function LocaisEstoqueSection() {
  const [nome, setNome] = useState('');
  const [filialId, setFilialId] = useState('');
  const [filiais, setFiliais] = useState<Filial[]>([]);
  const [erroFiliais, setErroFiliais] = useState<string | null>(null);
  const [filiaisCarregadas, setFiliaisCarregadas] = useState(false);
  const [estoques, setEstoques] = useState<Estoque[]>([]);
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [exclusaoPendente, setExclusaoPendente] = useState<{
    id: string;
    nome: string;
  } | null>(null);
  const [excluindo, setExcluindo] = useState(false);
  const [indicadores, setIndicadores] = useState<IndicadoresLocais | null>(null);

  const carregar = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/estoques'), {
        headers: authHeaders(),
      });
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { estoques: Estoque[] };
      setEstoques(body.estoques ?? []);
      setErroCarregar(null);
    } catch {
      setErroCarregar(MENSAGEM_ERRO_CARREGAR);
    }
  }, []);

  // Indicadores (Story 17.5): null até carregar ou se a rota falhar — a faixa
  // mostra "—" e a tela segue funcionando.
  const seqIndRef = useRef(0);
  const carregarIndicadores = useCallback(async () => {
    const seq = ++seqIndRef.current;
    try {
      const res = await fetch(apiUrl('/api/estoques/indicadores'), {
        headers: authHeaders(),
      });
      if (seq !== seqIndRef.current) return;
      if (!res.ok) {
        setIndicadores(null);
        return;
      }
      const corpo = (await res.json()) as IndicadoresLocais;
      if (seq !== seqIndRef.current) return;
      setIndicadores(corpo);
    } catch {
      if (seq === seqIndRef.current) setIndicadores(null);
    }
  }, []);

  const carregarFiliais = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/filiais'), {
        headers: authHeaders(),
      });
      if (!res.ok) {
        setErroFiliais(MENSAGEM_ERRO_CARREGAR_FILIAIS);
        return;
      }
      const body = (await res.json()) as { filiais?: Filial[] };
      const lista = body.filiais ?? [];
      setFiliais(lista);
      // Uma única Filial: já vem selecionada (não sobrescreve escolha do usuário).
      if (lista.length === 1) {
        setFilialId((atual) => (atual === '' ? lista[0].id : atual));
      }
      setErroFiliais(null);
      setFiliaisCarregadas(true);
    } catch {
      setErroFiliais(MENSAGEM_ERRO_CARREGAR_FILIAIS);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await Promise.all([carregar(), carregarFiliais(), carregarIndicadores()]);
    })();
  }, [carregar, carregarFiliais, carregarIndicadores]);

  async function enviar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    // Defesa em profundidade contra duplo-submit (molde de ConfiguracoesPage):
    // o `disabled` do botão só reflete `enviando` após o próximo repaint.
    if (enviando || nome.trim() === '' || filialId === '') {
      return;
    }
    setErro(null);
    setEnviando(true);
    try {
      const res = await fetch(apiUrl('/api/estoques'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ nome, filial_id: filialId }),
      });
      if (res.status === 409) {
        setErro(MENSAGEM_ERRO_DUPLICADO);
        return;
      }
      if (res.status === 400) {
        // Validação do servidor (nome/Filial inválidos): mostra a mensagem dele.
        const corpo = (await res.json().catch(() => ({}))) as {
          error?: { message?: string };
        };
        setErro(corpo.error?.message || MENSAGEM_ERRO_CADASTRO);
        return;
      }
      if (!res.ok) {
        setErro(MENSAGEM_ERRO_CADASTRO);
        return;
      }
      toast.success('Estoque criado.');
      setNome('');
      await Promise.all([carregar(), carregarIndicadores()]);
    } catch {
      setErro(MENSAGEM_ERRO_CADASTRO);
    } finally {
      setEnviando(false);
    }
  }

  async function excluir(id: string) {
    // Guard contra duplo disparo (molde de `executar` em GestaoUsuariosSection).
    if (excluindo) {
      return;
    }
    setErro(null);
    setExcluindo(true);
    try {
      const res = await fetch(apiUrl(`/api/estoques/${id}`), {
        method: 'DELETE',
        headers: authHeaders(),
      });
      if (res.status === 204 || res.ok) {
        toast.success('Estoque excluído.');
      } else if (res.status === 409) {
        // Guard de quantidade residual (Story 3.1): a mensagem do servidor já
        // cita os Produtos com resíduo — não é um texto fixo como o 409 de
        // nome duplicado do cadastro, porque varia por Estoque.
        const body = (await res.json().catch(() => ({}))) as {
          error?: { message?: string };
        };
        setErro(body.error?.message ?? MENSAGEM_ERRO_EXCLUIR);
      } else {
        setErro(MENSAGEM_ERRO_EXCLUIR);
      }
    } catch {
      setErro(MENSAGEM_ERRO_EXCLUIR);
    } finally {
      // Sucesso OU falha: recarrega para a linha refletir o estado real (ou a
      // linha obsoleta cair após um 404 de corrida).
      setExcluindo(false);
      setExclusaoPendente(null);
      await Promise.all([carregar(), carregarIndicadores()]);
    }
  }

  function confirmarExclusao() {
    if (!exclusaoPendente) {
      return;
    }
    const { id } = exclusaoPendente;
    setExclusaoPendente(null);
    void excluir(id);
  }

  return (
    <div className="flex flex-col gap-4">
      <h1 className="text-heading-lg">Locais</h1>

      <FaixaIndicadores
        indicadores={[
          { rotulo: 'Locais', valor: indicadores?.locais ?? null },
          {
            rotulo: 'Itens em estoque',
            valor: indicadores?.itensEmEstoque ?? null,
          },
        ]}
      />

      <form onSubmit={enviar} className="flex flex-col gap-2" noValidate>
        <Label htmlFor="estoque-filial">Filial</Label>
        <select
          id="estoque-filial"
          required
          value={filialId}
          onChange={(event) => setFilialId(event.target.value)}
          className="text-body min-h-touch-target-min rounded-md border border-border bg-background px-3"
        >
          <option value="">Selecione a filial</option>
          {filiais.map((f) => (
            <option key={f.id} value={f.id}>
              {f.nome}
            </option>
          ))}
        </select>
        {erroFiliais && (
          <p role="alert" className="text-body text-destructive">
            {erroFiliais}
          </p>
        )}
        {!erroFiliais && filiaisCarregadas && filiais.length === 0 && (
          <p role="alert" className="text-body text-destructive">
            {MENSAGEM_SEM_FILIAIS}
          </p>
        )}
        <Label htmlFor="estoque-nome">Nome do estoque</Label>
        <div className="flex gap-2">
          <Input id="estoque-nome" value={nome} onChange={(event) => setNome(event.target.value)} />
          <Button type="submit" disabled={enviando || nome.trim() === '' || filialId === ''}>
            {enviando ? 'Adicionando...' : 'Adicionar estoque'}
          </Button>
        </div>
        {erro && (
          <p role="alert" className="text-body text-destructive">
            {erro}
          </p>
        )}
      </form>

      {erroCarregar && (
        <p role="alert" className="text-body text-destructive">
          {erroCarregar}
        </p>
      )}
      {!erroCarregar && estoques.length === 0 && (
        <p className="text-body text-muted-foreground">Nenhum estoque cadastrado ainda.</p>
      )}
      {!erroCarregar && estoques.length > 0 && (
        <ul className="flex flex-col">
          {estoques.map((e) => (
            <li
              key={e.id}
              className="text-body flex min-h-[60px] flex-wrap items-center gap-3 border-b border-border"
            >
              <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted">
                <MapPin aria-hidden="true" className="h-4 w-4 text-muted-foreground" />
              </div>
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="min-w-0 break-words font-medium">{e.nome}</span>
                <span className="text-label text-muted-foreground">
                  Filial: {e.filial_nome ?? '—'}
                </span>
              </div>
              <span
                className="shrink-0 text-body tabular-nums"
                aria-label={`${e.totalItens ?? 0} itens`}
              >
                {e.totalItens ?? 0}
              </span>
              <Button
                type="button"
                variant="destructive"
                size="sm"
                className="shrink-0"
                aria-label={`Excluir estoque ${e.nome}`}
                onClick={() => setExclusaoPendente({ id: e.id, nome: e.nome })}
                disabled={excluindo}
              >
                Excluir
              </Button>
            </li>
          ))}
        </ul>
      )}

      <ConfirmDialog
        open={exclusaoPendente !== null}
        onOpenChange={(aberto) => {
          if (!aberto) {
            setExclusaoPendente(null);
          }
        }}
        onConfirm={confirmarExclusao}
        title={`Excluir o estoque "${exclusaoPendente?.nome ?? ''}"?`}
        description="O local é removido da lista. Esta ação não pode ser desfeita."
        confirmLabel="Excluir"
      />
    </div>
  );
}

export default LocaisEstoqueSection;
