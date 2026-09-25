import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { ImportacaoProdutosSection } from '@/components/produtos/ImportacaoProdutosSection';

/**
 * Página "Importar planilha" (`/produtos/importar`, Story 17.1 — antes a aba
 * "Importação" do Catálogo). Gate `almoxarife`+; o servidor é a autoridade
 * (`POST /api/importacoes` responde 403 abaixo do papel).
 */
export function ImportarProdutosPage() {
  const { usuario } = useAuth();
  const podeImportar = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeImportar ? (
        <>
          <h1 className="text-heading-lg">Importar planilha</h1>
          <ImportacaoProdutosSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso à importação de produtos.
        </p>
      )}
    </div>
  );
}

export default ImportarProdutosPage;
