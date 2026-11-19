import { useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Depósitos de UMA Filial (spec-depositos-na-filial; Empresa > Filial >
 * Depósito). O Depósito é a tabela `estoques`; a Filial é implícita (sem
 * seletor). Lista os depósitos recebidos, cadastra (`POST /api/estoques` com
 * `{ nome, filial_id }`) e exclui (`DELETE /api/estoques/{id}`) com
 * `ConfirmDialog`. Só é montado para `gestor`+ (o servidor também recusa com
 * 403). Após criar/excluir (e após falha de exclusão, para a linha refletir o
 * estado real) chama `onMudou` para o pai recarregar. Erro de cadastro
 * preserva o texto digitado; `409`/`400` mostram a mensagem do servidor ou a
 * fixa abaixo.
 */

export interface Deposito {
  id: string;
  nome: string;
  filial_id?: string | null;
}

interface Props {
  filialId: string;
  filialNome: string;
  depositos: Deposito[];
  onMudou: () => Promise<void> | void;
}

const MENSAGEM_ERRO_CADASTRO =
  'Não foi possível cadastrar o depósito agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_DUPLICADO = 'Já existe um depósito com esse nome.';
const MENSAGEM_JA_REMOVIDO = 'Este depósito já foi removido.';
const MENSAGEM_SEM_PERMISSAO = 'Você não tem permissão para excluir depósitos.';
const NOME_MAX = 255;
const MENSAGEM_ERRO_EXCLUIR =
  'Não foi possível excluir o depósito agora. Tente novamente em instantes.';

export function DepositosDaFilial({ filialId, filialNome, depositos, onMudou }: Props) {
  const [nome, setNome] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [exclusaoPendente, setExclusaoPendente] = useState<{
    id: string;
    nome: string;
  } | null>(null);
  const [excluindo, setExcluindo] = useState(false);

  async function adicionar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando || excluindo || nome.trim() === '') return;
    setErro(null);
    setEnviando(true);
    try {
      const res = await fetch(apiUrl('/api/estoques'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ nome: nome.trim(), filial_id: filialId }),
      });
      if (res.status === 409) {
        setErro(MENSAGEM_ERRO_DUPLICADO);
        return;
      }
      if (res.status === 400) {
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
    } catch {
      setErro(MENSAGEM_ERRO_CADASTRO);
      return;
    } finally {
      setEnviando(false);
    }
    // Cadastro gravado: o recarregamento da lista é isolado, para uma falha
    // dele nunca parecer erro de cadastro (e induzir a reenviar e duplicar).
    toast.success('Depósito criado.');
    setNome('');
    try {
      await onMudou();
    } catch {
      // o pai sinaliza a própria falha de carga
    }
  }

  async function excluir(id: string) {
    if (excluindo || enviando) return;
    setErro(null);
    setExcluindo(true);
    try {
      const res = await fetch(apiUrl(`/api/estoques/${id}`), {
        method: 'DELETE',
        headers: authHeaders(),
      });
      if (res.status === 204 || res.ok) {
        toast.success('Depósito excluído.');
      } else if (res.status === 404) {
        setErro(MENSAGEM_JA_REMOVIDO);
      } else if (res.status === 403) {
        setErro(MENSAGEM_SEM_PERMISSAO);
      } else if (res.status === 409) {
        // Resíduo de saldo ou Pedido pendente: a mensagem do servidor já cita
        // o motivo (varia por Depósito), por isso não é um texto fixo.
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
      setExcluindo(false);
      setExclusaoPendente(null);
      try {
        await onMudou();
      } catch {
        // o pai sinaliza a própria falha de carga
      }
    }
  }

  const idCampo = `deposito-nome-${filialId}`;

  return (
    <div className="flex flex-col gap-3">
      {depositos.length === 0 ? (
        <p className="text-body text-muted-foreground">Nenhum depósito nesta filial ainda.</p>
      ) : (
        <ul className="flex flex-col">
          {depositos.map((d) => (
            <li
              key={d.id}
              className="text-body flex min-h-[48px] items-center gap-3 border-b border-border"
            >
              <span className="min-w-0 flex-1 break-words font-medium">{d.nome}</span>
              <Button
                type="button"
                variant="destructive"
                size="sm"
                className="shrink-0"
                aria-label={`Excluir depósito ${d.nome}`}
                disabled={excluindo || enviando}
                onClick={() => setExclusaoPendente({ id: d.id, nome: d.nome })}
              >
                Excluir
              </Button>
            </li>
          ))}
        </ul>
      )}
      <form onSubmit={adicionar} className="flex flex-col gap-2" noValidate>
        <Label htmlFor={idCampo}>Nome do depósito em {filialNome}</Label>
        <div className="flex gap-2">
          <Input
            id={idCampo}
            value={nome}
            maxLength={NOME_MAX}
            onChange={(event) => setNome(event.target.value)}
          />
          <Button type="submit" disabled={enviando || excluindo || nome.trim() === ''}>
            {enviando ? 'Adicionando...' : 'Adicionar depósito'}
          </Button>
        </div>
        {erro && (
          <p role="alert" className="text-body text-destructive">
            {erro}
          </p>
        )}
      </form>
      <ConfirmDialog
        open={exclusaoPendente !== null}
        onOpenChange={(aberto) => {
          if (!aberto) setExclusaoPendente(null);
        }}
        onConfirm={() => {
          if (!exclusaoPendente) return;
          const { id } = exclusaoPendente;
          setExclusaoPendente(null);
          void excluir(id);
        }}
        title={`Excluir o depósito "${exclusaoPendente?.nome ?? ''}"?`}
        description="O depósito é removido da lista. Esta ação não pode ser desfeita."
        confirmLabel="Excluir"
      />
    </div>
  );
}

export default DepositosDaFilial;
