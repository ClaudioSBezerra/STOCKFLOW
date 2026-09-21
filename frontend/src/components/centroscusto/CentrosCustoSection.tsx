import { useCallback, useEffect, useState, type FormEvent } from 'react';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Seção "Centros de custo" de `/configuracoes` (Story 12.3, spec-12-3, FR-51, AD-28). Só é
 * montada para `adm`+ (gate em `ConfiguracoesPage`; o servidor também recusa
 * com 403 abaixo de `adm`).
 *
 * Lista `GET /api/centros-custo` e cadastra (`POST /api/centros-custo` com `{ nome }`).
 * Sem editar/excluir Centro de Custo nesta story. `maxLength` 255 espelha o banco.
 * `400`/`409` mostram a mensagem do PRÓPRIO servidor em `<p role="alert">`;
 * a lista é recarregada após cada cadastro.
 */

interface CentroCusto {
  id: string;
  nome: string;
}

const NOME_MAX = 255;

const MENSAGEM_ERRO_CARREGAR = 'Não foi possível carregar os centros de custo. Recarregue a página.';
const MENSAGEM_ERRO_RECARREGAR_APOS_CADASTRO =
  'Centro de custo criado, mas não foi possível atualizar a lista. Recarregue a página — não cadastre de novo.';
const MENSAGEM_ERRO_CADASTRO =
  'Não foi possível cadastrar o centro de custo agora. Tente novamente em instantes.';

async function mensagemDoServidor(res: Response, padrao: string): Promise<string> {
  const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
  return body.error?.message || padrao;
}

export function CentrosCustoSection() {
  const [centros, setCentros] = useState<CentroCusto[]>([]);
  const [nome, setNome] = useState('');
  const [enviando, setEnviando] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [erroCarregar, setErroCarregar] = useState<string | null>(null);
  const [carregou, setCarregou] = useState(false);

  const carregar = useCallback(async (mensagemErro: string = MENSAGEM_ERRO_CARREGAR) => {
    try {
      const res = await fetch(apiUrl('/api/centros-custo'), { headers: authHeaders() });
      if (!res.ok) {
        setErroCarregar(mensagemErro);
        return;
      }
      const body = (await res.json()) as { centrosCusto: CentroCusto[] };
      setCentros(body.centrosCusto ?? []);
      setErroCarregar(null);
      setCarregou(true);
    } catch {
      setErroCarregar(mensagemErro);
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
      const res = await fetch(apiUrl('/api/centros-custo'), {
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
      toast.success('Centro de custo criado.');
      setNome('');
      await carregar(MENSAGEM_ERRO_RECARREGAR_APOS_CADASTRO);
    } catch {
      setErro(MENSAGEM_ERRO_CADASTRO);
    } finally {
      setEnviando(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Centros de custo</h2>
        <CardDescription>
          Cadastre os centros de custo (obras e destinos) da sua empresa. Quem envia um pedido pode escolher um deles.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <form onSubmit={cadastrar} className="flex flex-col gap-2" noValidate>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
            <div className="flex flex-1 flex-col gap-2">
              <Label htmlFor="centro-custo-nome">Nome do centro de custo</Label>
              <Input
                id="centro-custo-nome"
                value={nome}
                maxLength={NOME_MAX}
                onChange={(event) => setNome(event.target.value)}
              />
            </div>
            <Button type="submit" disabled={enviando || nome.trim() === ''}>
              {enviando ? 'Adicionando...' : 'Adicionar centro de custo'}
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
        {!erroCarregar && carregou && centros.length === 0 && (
          <p className="text-body text-muted-foreground">Nenhum centro de custo cadastrado ainda.</p>
        )}
        {!erroCarregar && centros.length > 0 && (
          <ul className="flex flex-col gap-2">
            {centros.map((f) => (
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

export default CentrosCustoSection;
