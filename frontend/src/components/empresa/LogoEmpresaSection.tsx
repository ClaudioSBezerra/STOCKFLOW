import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from 'sonner';
import { ConfirmDialog } from '@/components/ConfirmDialog';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card';
import { apiUrl, authHeaders } from '@/lib/api';

/**
 * Seção "Logo da Empresa" de `/cadastros/empresa` (spec-logo-da-empresa).
 * Só é montada para `gestor`+ (gate em `EmpresaPage`; o servidor também
 * recusa com 403 abaixo de `gestor`).
 *
 * Mostra a logo atual (`GET /api/empresa/logo`, 404 = nenhuma), deixa
 * escolher um arquivo com pré-visualização ANTES de confirmar o envio
 * (`PUT /api/empresa/logo`, multipart campo `logo`) e remover
 * (`DELETE /api/empresa/logo`, com confirmação). A validação real (PNG ou
 * JPEG de verdade, até 512 KB, 32 a 4000 px) é do servidor; a mensagem de
 * 400 dele aparece em `<p role="alert">`. A logo sai na Pré-Nota.
 */

const MENSAGEM_FORMATO = 'Formato inválido: use PNG ou JPEG.';
const MENSAGEM_TAMANHO = 'A logo é grande demais: o limite é 512 KB.';
const TAMANHO_MAX_BYTES = 512 * 1024;
const MENSAGEM_VALE_PARA_TODAS = 'A logo vale para todas as Pré-Notas, inclusive as já emitidas.';
const MENSAGEM_ERRO_CARREGAR = 'Não foi possível carregar a logo. Recarregue a página.';
const MENSAGEM_ERRO_ENVIO = 'Não foi possível enviar a logo agora. Tente novamente em instantes.';
const MENSAGEM_ERRO_REMOCAO = 'Não foi possível remover a logo agora. Tente novamente em instantes.';

async function mensagemDoServidor(res: Response, padrao: string): Promise<string> {
  const body = (await res.json().catch(() => ({}))) as { error?: { message?: string } };
  return body.error?.message || padrao;
}

export function LogoEmpresaSection() {
  // URL (blob:) da logo gravada; null = nenhuma.
  const [logoUrl, setLogoUrl] = useState<string | null>(null);
  const [carregou, setCarregou] = useState(false);
  const [erro, setErro] = useState<string | null>(null);
  // A última busca da logo falhou (não foi 404): mostra "Tentar de novo".
  const [falhouCarregar, setFalhouCarregar] = useState(false);
  // Arquivo escolhido aguardando confirmação, e a URL da sua pré-visualização.
  const [arquivo, setArquivo] = useState<File | null>(null);
  const [previaUrl, setPreviaUrl] = useState<string | null>(null);
  const [enviando, setEnviando] = useState(false);
  const [confirmandoRemocao, setConfirmandoRemocao] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  // Cada busca ganha um número; só a mais recente pode mexer no estado.
  const buscaAtual = useRef(0);

  const limparInput = useCallback(() => {
    if (inputRef.current) inputRef.current.value = '';
  }, []);

  const carregar = useCallback(async () => {
    const minha = ++buscaAtual.current;
    const atual = () => minha === buscaAtual.current;
    setFalhouCarregar(false);
    try {
      const res = await fetch(apiUrl('/api/empresa/logo'), {
        headers: authHeaders(),
        cache: 'no-cache',
      });
      if (!atual()) return;
      if (res.status === 404) {
        setLogoUrl(null);
        setErro(null);
        setCarregou(true);
        return;
      }
      if (!res.ok) {
        setErro(MENSAGEM_ERRO_CARREGAR);
        setFalhouCarregar(true);
        setCarregou(true);
        return;
      }
      const blob = await res.blob();
      if (!atual()) return; // resposta atrasada: não cria URL de objeto à toa
      setLogoUrl(URL.createObjectURL(blob));
      setErro(null);
      setCarregou(true);
    } catch {
      if (!atual()) return;
      setErro(MENSAGEM_ERRO_CARREGAR);
      setFalhouCarregar(true);
      setCarregou(true);
    }
  }, []);

  useEffect(() => {
    void (async () => {
      await carregar();
    })();
  }, [carregar]);

  // Libera as URLs de objeto quando trocam ou a seção desmonta.
  useEffect(() => {
    return () => {
      if (logoUrl) URL.revokeObjectURL(logoUrl);
    };
  }, [logoUrl]);
  useEffect(() => {
    return () => {
      if (previaUrl) URL.revokeObjectURL(previaUrl);
    };
  }, [previaUrl]);

  function escolher(file: File | undefined) {
    setErro(null);
    if (!file) return;
    // Mesmas regras e mensagens do servidor, antes de gastar o envio.
    if (file.type !== 'image/png' && file.type !== 'image/jpeg') {
      setErro(MENSAGEM_FORMATO);
      limparInput();
      return;
    }
    if (file.size > TAMANHO_MAX_BYTES) {
      setErro(MENSAGEM_TAMANHO);
      limparInput();
      return;
    }
    setArquivo(file);
    setPreviaUrl(URL.createObjectURL(file));
  }

  function descartar() {
    setArquivo(null);
    setPreviaUrl(null);
    setErro(null);
    limparInput();
  }

  async function enviar() {
    if (!arquivo || enviando) return;
    setEnviando(true);
    setErro(null);
    try {
      const corpo = new FormData();
      corpo.append('logo', arquivo);
      const res = await fetch(apiUrl('/api/empresa/logo'), {
        method: 'PUT',
        headers: authHeaders(),
        body: corpo,
      });
      if (!res.ok) {
        setErro(await mensagemDoServidor(res, MENSAGEM_ERRO_ENVIO));
        limparInput();
        return;
      }
      toast.success(`Logo salva. ${MENSAGEM_VALE_PARA_TODAS}`);
      descartar();
      await carregar();
    } catch {
      setErro(MENSAGEM_ERRO_ENVIO);
      limparInput();
    } finally {
      setEnviando(false);
    }
  }

  async function remover() {
    setEnviando(true);
    setErro(null);
    try {
      const res = await fetch(apiUrl('/api/empresa/logo'), {
        method: 'DELETE',
        headers: authHeaders(),
      });
      if (!res.ok) {
        setErro(await mensagemDoServidor(res, MENSAGEM_ERRO_REMOCAO));
        return;
      }
      toast.success('Logo removida. As Pré-Notas, inclusive as já emitidas, saem sem logo.');
      buscaAtual.current++; // uma busca em andamento não ressuscita a logo
      setLogoUrl(null);
    } catch {
      setErro(MENSAGEM_ERRO_REMOCAO);
    } finally {
      setEnviando(false);
    }
  }

  return (
    <Card>
      <CardHeader>
        <h2 className="text-heading-md">Logo da Empresa</h2>
        <CardDescription>
          Aparece no canto do cabeçalho da Pré-Nota impressa, para todas as Filiais.{' '}
          {MENSAGEM_VALE_PARA_TODAS} Formato PNG ou JPEG comum, até 512 KB, entre 32 e 4000 pixels de
          largura e altura e no máximo 4 milhões de pixels (ex.: 2000×2000); prefira fundo
          transparente ou branco.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <div className="flex min-h-24 items-center justify-center rounded-md border border-border bg-white p-4">
          {logoUrl ? (
            <img src={logoUrl} alt="Logo atual da Empresa" className="max-h-24 max-w-full" />
          ) : falhouCarregar ? (
            <Button type="button" variant="outline" onClick={() => void carregar()}>
              Tentar de novo
            </Button>
          ) : carregou ? (
            <p className="text-body text-muted-foreground">Nenhuma logo cadastrada.</p>
          ) : (
            <p className="text-body text-muted-foreground">Carregando…</p>
          )}
        </div>

        {previaUrl && arquivo && (
          <div className="flex flex-col gap-2">
            <p className="text-body font-medium">Pré-visualização de {arquivo.name}</p>
            <div className="flex min-h-24 items-center justify-center rounded-md border border-dashed border-border bg-white p-4">
              <img src={previaUrl} alt="Pré-visualização da nova logo" className="max-h-24 max-w-full" />
            </div>
          </div>
        )}

        {erro && (
          <p role="alert" className="text-body text-destructive">
            {erro}
          </p>
        )}

        <input
          ref={inputRef}
          type="file"
          accept="image/png,image/jpeg"
          aria-label="Arquivo da logo"
          className="sr-only"
          onChange={(e) => escolher(e.target.files?.[0])}
        />
        <div className="flex flex-wrap gap-2">
          {arquivo ? (
            <>
              <Button type="button" disabled={enviando} onClick={() => void enviar()}>
                {enviando ? 'Enviando…' : 'Confirmar envio'}
              </Button>
              <Button type="button" variant="outline" disabled={enviando} onClick={descartar}>
                Cancelar
              </Button>
            </>
          ) : (
            <>
              <Button
                type="button"
                disabled={enviando}
                onClick={() => inputRef.current?.click()}
              >
                {logoUrl ? 'Trocar logo' : 'Enviar logo'}
              </Button>
              {logoUrl && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={enviando}
                  onClick={() => setConfirmandoRemocao(true)}
                >
                  Remover logo
                </Button>
              )}
            </>
          )}
        </div>
      </CardContent>
      <ConfirmDialog
        open={confirmandoRemocao}
        onOpenChange={setConfirmandoRemocao}
        onConfirm={() => void remover()}
        title="Remover a logo?"
        description="As Pré-Notas passam a sair sem logo, inclusive as já emitidas."
        confirmLabel="Remover"
        confirmVariant="destructive"
      />
    </Card>
  );
}
