import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Seção "Fotos" do diálogo "Editar produto": gerencia as fotos de um Produto
 * JÁ cadastrado — antes, só o cadastro (logo após o `201`) permitia enviar
 * foto, e um Produto existente (inclusive os de exemplo do Treinamento) não
 * tinha como receber, trocar ou perder uma foto pela tela.
 *
 * - Galeria: `GET /api/produtos/{id}/fotos` + uma busca autenticada por foto
 *   (`<img src>` não manda o header de auth), Object URLs revogados ao
 *   desmontar.
 * - Adicionar: `POST /api/produtos/{id}/fotos` (multipart, campo `foto`); o
 *   servidor redimensiona/recomprime, como no cadastro.
 * - Remover: `DELETE /api/produtos/{id}/fotos/{nome}` depois de confirmar.
 * - Trocar: envia a nova PRIMEIRO e só então remove a antiga — se o envio
 *   falhar, a foto antiga continua lá (nunca deixa o Produto sem a foto).
 *
 * Papel mínimo `almoxarife` (o mesmo do diálogo). `onAlterado` avisa quem
 * abriu o diálogo para recarregar a própria galeria.
 */

const ACEITOS = 'image/jpeg,image/png,image/webp';
const MENSAGEM_ERRO_GENERICA = 'Não foi possível concluir agora. Tente novamente em instantes.';

interface Foto {
  nome: string;
  url: string;
  objectUrl: string;
}

interface FotosProdutoSectionProps {
  produtoId: string;
  onAlterado?: () => void;
}

async function mensagemDoServidor(res: Response): Promise<string> {
  const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
  return body.error?.message ?? MENSAGEM_ERRO_GENERICA;
}

export function FotosProdutoSection({ produtoId, onAlterado }: FotosProdutoSectionProps) {
  const [fotos, setFotos] = useState<Foto[]>([]);
  const [carregando, setCarregando] = useState(true);
  const [ocupado, setOcupado] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  const [arquivoNovo, setArquivoNovo] = useState<File | null>(null);
  const [inputKey, setInputKey] = useState(0);
  const [removendo, setRemovendo] = useState<string | null>(null);
  const cacheRef = useRef<Map<string, string>>(new Map());
  const trocarInputRef = useRef<HTMLInputElement>(null);
  const trocandoRef = useRef<string | null>(null);

  const carregar = useCallback(async () => {
    try {
      const res = await fetch(apiUrl(`/api/produtos/${produtoId}/fotos`), { headers: authHeaders() });
      if (!res.ok) {
        setErro('Não foi possível carregar as fotos agora.');
        return;
      }
      const body = (await res.json()) as { fotos: { nome: string; url: string }[] };
      const itens: Foto[] = [];
      for (const foto of body.fotos) {
        let objectUrl = cacheRef.current.get(foto.nome);
        if (!objectUrl) {
          const resFoto = await fetch(apiUrl(foto.url), { headers: authHeaders() });
          if (!resFoto.ok) continue;
          objectUrl = URL.createObjectURL(await resFoto.blob());
          cacheRef.current.set(foto.nome, objectUrl);
        }
        itens.push({ nome: foto.nome, url: foto.url, objectUrl });
      }
      // Revoga o Object URL de foto que saiu da lista (removida/trocada).
      const vigentes = new Set(itens.map((f) => f.nome));
      for (const [nome, url] of cacheRef.current) {
        if (!vigentes.has(nome)) {
          URL.revokeObjectURL(url);
          cacheRef.current.delete(nome);
        }
      }
      setFotos(itens);
    } catch {
      setErro('Não foi possível carregar as fotos agora.');
    } finally {
      setCarregando(false);
    }
  }, [produtoId]);

  useEffect(() => {
    void carregar();
    const cache = cacheRef.current;
    return () => {
      for (const url of cache.values()) URL.revokeObjectURL(url);
      cache.clear();
    };
  }, [carregar]);

  async function enviar(arquivo: File): Promise<boolean> {
    const formData = new FormData();
    formData.append('foto', arquivo);
    const res = await fetch(apiUrl(`/api/produtos/${produtoId}/fotos`), {
      method: 'POST',
      headers: authHeaders(),
      body: formData,
    });
    if (!res.ok) {
      setErro(await mensagemDoServidor(res));
      return false;
    }
    return true;
  }

  async function remover(nome: string): Promise<boolean> {
    const res = await fetch(apiUrl(`/api/produtos/${produtoId}/fotos/${nome}`), {
      method: 'DELETE',
      headers: authHeaders(),
    });
    if (!res.ok && res.status !== 404) {
      setErro(await mensagemDoServidor(res));
      return false;
    }
    return true;
  }

  async function executar(acao: () => Promise<boolean>, sucesso: string) {
    setErro(null);
    setOcupado(true);
    try {
      if (await acao()) {
        toast.success(sucesso);
        onAlterado?.();
      }
    } catch {
      setErro(MENSAGEM_ERRO_GENERICA);
    } finally {
      await carregar();
      setOcupado(false);
    }
  }

  function adicionar() {
    if (!arquivoNovo || ocupado) return;
    const arquivo = arquivoNovo;
    void executar(async () => {
      const ok = await enviar(arquivo);
      if (ok) {
        setArquivoNovo(null);
        setInputKey((k) => k + 1);
      }
      return ok;
    }, 'Foto adicionada.');
  }

  function iniciarTroca(nome: string) {
    trocandoRef.current = nome;
    trocarInputRef.current?.click();
  }

  function concluirTroca(arquivo: File | undefined) {
    const antiga = trocandoRef.current;
    trocandoRef.current = null;
    if (trocarInputRef.current) trocarInputRef.current.value = '';
    if (!arquivo || !antiga) return;
    // Envia a nova primeiro: se falhar, a antiga continua no Produto.
    void executar(async () => (await enviar(arquivo)) && (await remover(antiga)), 'Foto trocada.');
  }

  function confirmarRemocao() {
    const nome = removendo;
    setRemovendo(null);
    if (!nome) return;
    void executar(() => remover(nome), 'Foto removida.');
  }

  return (
    <section aria-labelledby="fotos-produto-titulo" className="flex flex-col gap-3 border-t pt-4">
      <h3 id="fotos-produto-titulo" className="text-label font-semibold">
        Fotos
      </h3>

      {carregando ? (
        <p className="text-body text-muted-foreground">Carregando fotos...</p>
      ) : fotos.length === 0 ? (
        <p className="text-body text-muted-foreground">Este produto ainda não tem foto.</p>
      ) : (
        <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3">
          {fotos.map((foto, i) => (
            <li key={foto.nome} className="flex flex-col gap-2">
              <img
                src={foto.objectUrl}
                alt={`Foto ${i + 1} do produto`}
                className="aspect-square w-full rounded-md border object-cover"
              />
              <div className="flex gap-2">
                <Button
                  type="button"
                  variant="outline"
                  disabled={ocupado}
                  onClick={() => iniciarTroca(foto.nome)}
                  aria-label={`Trocar foto ${i + 1}`}
                >
                  Trocar
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  disabled={ocupado}
                  onClick={() => setRemovendo(foto.nome)}
                  aria-label={`Remover foto ${i + 1}`}
                >
                  Remover
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {/* Input oculto compartilhado pelos botões "Trocar". */}
      <input
        ref={trocarInputRef}
        type="file"
        accept={ACEITOS}
        className="hidden"
        data-testid="fotos-trocar-input"
        onChange={(e) => concluirTroca(e.target.files?.[0])}
      />

      <div className="flex flex-col gap-2">
        <Label htmlFor="fotos-produto-nova">Adicionar foto</Label>
        <div className="flex flex-col gap-2 sm:flex-row">
          <Input
            key={inputKey}
            id="fotos-produto-nova"
            type="file"
            accept={ACEITOS}
            disabled={ocupado}
            onChange={(e) => setArquivoNovo(e.target.files?.[0] ?? null)}
          />
          <Button type="button" onClick={adicionar} disabled={!arquivoNovo || ocupado}>
            {ocupado ? 'Enviando...' : 'Enviar foto'}
          </Button>
        </div>
      </div>

      {erro && (
        <p role="alert" className="text-body text-destructive">
          {erro}
        </p>
      )}

      <ConfirmDialog
        open={removendo !== null}
        onOpenChange={(open) => {
          if (!open) setRemovendo(null);
        }}
        onConfirm={confirmarRemocao}
        title="Remover esta foto?"
        description="A foto é apagada do produto. Para trocar por outra, use Trocar."
        confirmLabel="Remover"
        confirmVariant="destructive"
      />
    </section>
  );
}
