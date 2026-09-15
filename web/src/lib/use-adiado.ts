"use client";

import { useEffect, useState } from "react";

// Devolve o valor só depois de `ms` sem mudanças (busca enquanto digita).
export function useAdiado<T>(valor: T, ms = 300): T {
  const [adiado, setAdiado] = useState(valor);
  useEffect(() => {
    const t = setTimeout(() => setAdiado(valor), ms);
    return () => clearTimeout(t);
  }, [valor, ms]);
  return adiado;
}
