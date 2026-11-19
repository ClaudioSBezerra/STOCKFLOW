import { rotuloCompletoMotivo, type MotivoSaida } from '@/lib/motivosSaida';

interface DetalheSaidaPedidoProps {
  tipo?: string;
  motivoSaida?: MotivoSaida | null;
  observacao?: string | null;
}

/**
 * Motivo (rótulo completo) e justificativa de um Pedido de Outras Saídas
 * (`tipo === 'baixa'`), mostrados ACIMA dos itens no Dialog de detalhe da Fila
 * e de Meus Pedidos. Outros tipos não renderizam nada.
 */
export function DetalheSaidaPedido({ tipo, motivoSaida, observacao }: DetalheSaidaPedidoProps) {
  if (tipo !== 'baixa') {
    return null;
  }
  return (
    <div className="flex flex-col gap-1">
      <p className="text-body">
        <span className="text-muted-foreground">Motivo: </span>
        {rotuloCompletoMotivo(motivoSaida)}
      </p>
      <p className="text-body break-words">
        <span className="text-muted-foreground">Justificativa: </span>
        {observacao?.trim() ? observacao : 'Não informada'}
      </p>
    </div>
  );
}

export default DetalheSaidaPedido;
