/** Datas do resumo diário: sempre como texto `AAAA-MM-DD`, sem `Date` local no meio. */

/**
 * Data de check-in/check-out do payload → `AAAA-MM-DD`. O provedor real manda `dd/mm/aaaa`;
 * ISO (`2026-10-26`, com ou sem hora) também vale. Qualquer outra coisa (ou data que não
 * existe, como 31/02) volta undefined: a reserva não entra no resumo e segue avulsa.
 */
export function lerData(valor: string | undefined): string | undefined {
  if (!valor) return undefined
  const texto = valor.trim()
  const br = /^(\d{2})\/(\d{2})\/(\d{4})$/.exec(texto)
  const iso = /^(\d{4})-(\d{2})-(\d{2})(?:$|[T ])/.exec(texto)
  let ano: number, mes: number, dia: number
  if (br) [ano, mes, dia] = [Number(br[3]), Number(br[2]), Number(br[1])]
  else if (iso) [ano, mes, dia] = [Number(iso[1]), Number(iso[2]), Number(iso[3])]
  else return undefined

  const data = new Date(Date.UTC(ano, mes - 1, dia))
  if (data.getUTCFullYear() !== ano || data.getUTCMonth() !== mes - 1 || data.getUTCDate() !== dia) {
    return undefined
  }
  return data.toISOString().slice(0, 10)
}

/** `AAAA-MM-DD` + n dias (n pode ser negativo). */
export function somarDias(data: string, dias: number): string {
  const d = new Date(`${data}T00:00:00Z`)
  d.setUTCDate(d.getUTCDate() + dias)
  return d.toISOString().slice(0, 10)
}

/** Dia e hora de `agora` no fuso dado (o relógio da pousada, não o do servidor). */
export function relogioLocal(agora: Date, fuso: string): { data: string; hora: number } {
  const partes = Object.fromEntries(
    new Intl.DateTimeFormat('en-CA', {
      timeZone: fuso,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      hourCycle: 'h23',
    })
      .formatToParts(agora)
      .map((p) => [p.type, p.value]),
  )
  return { data: `${partes.year}-${partes.month}-${partes.day}`, hora: Number(partes.hour) }
}
