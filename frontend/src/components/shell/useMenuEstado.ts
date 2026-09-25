import { useCallback, useState } from 'react';

// Estado do menu lateral lembrado no navegador, por pessoa. É só conveniência
// (Epic 17): toda leitura/escrita de `localStorage` fica em try/catch e, sem
// ele, o padrão é menu expandido com todos os grupos abertos.

function chave(nome: string, usuarioId: string): string {
  return `stockflow:menu:${nome}:${usuarioId}`;
}

function ler(nome: string, usuarioId: string): string | null {
  try {
    return window.localStorage.getItem(chave(nome, usuarioId));
  } catch {
    return null;
  }
}

function gravar(nome: string, usuarioId: string, valor: string): void {
  try {
    window.localStorage.setItem(chave(nome, usuarioId), valor);
  } catch {
    // Sem armazenamento: o menu funciona igual, só não lembra.
  }
}

function lerGrupos(usuarioId: string): Record<string, boolean> {
  const bruto = ler('grupos', usuarioId);
  if (!bruto) return {};
  try {
    const dado: unknown = JSON.parse(bruto);
    if (dado && typeof dado === 'object' && !Array.isArray(dado)) {
      return Object.fromEntries(
        Object.entries(dado).filter((par): par is [string, boolean] => typeof par[1] === 'boolean'),
      );
    }
  } catch {
    // JSON corrompido: ignora e usa o padrão.
  }
  return {};
}

export interface MenuEstado {
  recolhido: boolean;
  alternarRecolhido: () => void;
  /** Grupo aberto? Padrão aberto quando nada foi lembrado. */
  grupoAberto: (grupoId: string) => boolean;
  alternarGrupo: (grupoId: string) => void;
  abrirGrupo: (grupoId: string) => void;
}

export function useMenuEstado(usuarioId: string): MenuEstado {
  const [recolhido, setRecolhido] = useState(() => ler('recolhido', usuarioId) === '1');
  const [grupos, setGrupos] = useState<Record<string, boolean>>(() => lerGrupos(usuarioId));

  const alternarRecolhido = useCallback(() => {
    setRecolhido((atual) => {
      const novo = !atual;
      gravar('recolhido', usuarioId, novo ? '1' : '0');
      return novo;
    });
  }, [usuarioId]);

  const definirGrupo = useCallback(
    (grupoId: string, aberto: (atual: boolean) => boolean) => {
      setGrupos((atual) => {
        const valorAtual = atual[grupoId] !== false;
        const novoValor = aberto(valorAtual);
        if (novoValor === valorAtual) return atual;
        const novo = { ...atual, [grupoId]: novoValor };
        gravar('grupos', usuarioId, JSON.stringify(novo));
        return novo;
      });
    },
    [usuarioId],
  );

  const alternarGrupo = useCallback(
    (grupoId: string) => definirGrupo(grupoId, (atual) => !atual),
    [definirGrupo],
  );
  const abrirGrupo = useCallback(
    (grupoId: string) => definirGrupo(grupoId, () => true),
    [definirGrupo],
  );
  const grupoAberto = useCallback((grupoId: string) => grupos[grupoId] !== false, [grupos]);

  return { recolhido, alternarRecolhido, grupoAberto, alternarGrupo, abrirGrupo };
}
