import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Seção "Filiais" de `/configuracoes` (Story 12.1, spec-12-1, FR-51). Só é
 * montada para `adm`+ (gate em `ConfiguracoesPage`; o servidor também recusa
 * com 403 abaixo de `adm`).
 *
 * Lista `GET /api/filiais` e cadastra (`POST /api/filiais` com `{ nome }`).
 * Sem editar/excluir Filial nesta story. `maxLength` 255 espelha o banco.
 * `400`/`409` mostram a mensagem do PRÓPRIO servidor em `<p role="alert">`;
 * a lista é recarregada após cada cadastro.
 */

interface Filial {
  id: string;
  nome: string;
}

const NOME_MAX = 255;

const MENSAGEM_ERRO_CARREGAR = 'Não foi possível carregar as filiais. Recarregue a página.';
const MENSAGEM_ERRO_CADASTRO =
  'Não foi possível cadastrar a filial agora. Tente novamente em instantes.';

async function mensagemDoServidor(res: Response, padrao: string): Promise<string> {
  const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
  return body.error?.message || padrao;
}

export function FiliaisSection() {
  const [filiais, setFiliais] = useState<Filial[]>([]);
  const [nome, setNome] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [carregou, setCarregou] = useState(false);

  const carregar = useCallback(async () => {
    try {
      const res = await fetch(apiUrl('/api/filiais'), { headers: authHeaders() });
      if (!res.ok) {
        setErroCarregar(MENSAGEM_ERRO_CARREGAR);
        return;
      }
      const body = (await res.json()) as { filiais: Filial[] };
      setFiliais(body.filiais ?? []);
      setErroCarregar(null);
      setCarregou(true);
    } catch {
      setErroCarregar(MENSAGEM_ERRO_CARREGAR);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await carregar();
    })();
  }, [carregar]);

  async function cadastrar(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (enviando || nome.trim() === '') {
      return;
    }
    setErro(null);
    setEnviando(true);
    try {
      const res = await fetch(apiUrl('/api/filiais'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...authHeaders() },
        body: JSON.stringify({ nome }),
      });
      if (res.status === 400 || res.status === 409) {
        setErro(await mensagemDoServidor(res, MENSAGEM_ERRO_CADASTRO));
        return;
      }
      if (!res.ok) {
        setErro(MENSAGEM_ERRO_CADASTRO);
        return;
      }
      toast.success('Filial criada.');
      setNome('');
      await carregar();
    } catch {
      setErro(MENSAGEM_ERRO_CADASTRO);
    } finally {
      setEnviando(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Filiais</h2>
        <CardDescription>
          Cadastre as filiais da sua empresa. Cada estoque é vinculado a uma filial.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form onSubmit={cadastrar} className="flex flex-col gap-2" noValidate>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
            <div className="flex flex-1 flex-col gap-2">
              <Label htmlFor="filial-nome">Nome da filial</Label>
              <Input
                id="filial-nome"
                value={nome}
                maxLength={NOME_MAX}
                onChange={(event) => setNome(event.target.value)}
              />
            </div>
            <Button type="submit" disabled={enviando || nome.trim() === ''}>
              {enviando ? 'Adicionando...' : 'Adicionar filial'}
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
        {!erroCarregar && carregou && filiais.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhuma filial cadastrada ainda.</p>
        )}
        {!erroCarregar && filiais.length > 0 && (
          <ul className="flex flex-col gap-2">
            {filiais.map((f) => (
              <li
                key={f.id}
                className="text-body min-w-0 break-words border-b border-border pb-2 last:border-b-0 last:pb-0"
              >
                {f.nome}
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}

export default FiliaisSection;
