import { useAuth } from '@/lib/auth';
import { rankPapel } from '@/components/shell/nav-items';
import { CadastroProdutoSection } from '@/components/produtos/CadastroProdutoSection';

/**
 * Página "Cadastrar produto" (`/produtos/novo`, Story 17.1 — antes a aba
 * "Cadastro" do Catálogo). Gate `almoxarife`+; o servidor é a autoridade
 * (`POST /api/produtos` responde 403 abaixo do papel).
 */
export function CadastrarProdutoPage() {
  const { usuario } = useAuth();
  const podeCadastrar = rankPapel(usuario?.papel ?? '') >= rankPapel('almoxarife');

  return (
    <div className="flex flex-col gap-6 p-6">
      {podeCadastrar ? (
        <>
          <h1 className="text-heading-lg">Cadastrar produto</h1>
          <CadastroProdutoSection />
        </>
      ) : (
        <p className="text-body text-muted-foreground">
          Você não tem acesso ao cadastro de produtos.
        </p>
      )}
    </div>
  );
}

export default CadastrarProdutoPage;
