'use strict';

/**
 * Leitura dos diálogos JS do Newcon (alert/confirm), que o NewconClient aceita e registra.
 */

// "Oferta de Lance só poderá ser realizada até 02:30 hora(s) antes da assembleia.
//  Término da oferta de lance: 15/09/2026 à(s) 14:00 hora(s)." (visto em 2026-09-15, depois
// do prazo, ao clicar em "Localizar"; a página fica no filtro).
function prazoEncerrado(dialogos) {
  const aviso = dialogos.find((d) => /Oferta de Lance s[oó] poder[aá] ser realizada/i.test(d.mensagem || ''));
  if (!aviso) return null;
  const termino = /T[eé]rmino da oferta de lance:\s*(\d{2}\/\d{2}\/\d{4})\s*[àa]\(s\)\s*(\d{1,2}:\d{2})/i.exec(aviso.mensagem);
  return termino
    ? `prazo de oferta de lance encerrado (término em ${termino[1]} às ${termino[2]})`
    : `prazo de oferta de lance encerrado: ${aviso.mensagem.replace(/\s+/g, ' ').trim()}`;
}

// "Anote os números dos protocolos: 2º Lance Fixo (Automático): 1889071"
function extrairProtocolo(dialogos) {
  const aviso = dialogos.find((d) => /Anote os n[uú]meros dos protocolos/i.test(d.mensagem || ''));
  if (!aviso) return null;
  const numeros = aviso.mensagem.match(/\d{5,}/g);
  return numeros ? { protocolo: numeros[numeros.length - 1], texto: aviso.mensagem.replace(/\s+/g, ' ').trim() } : null;
}

const parcelasEmAtraso = (dialogos) => dialogos.some((d) => /Parcelas em Atraso/i.test(d.mensagem || ''));
const jaCredenciado = (dialogos) => dialogos.some((d) => /j[aá] credenciado nesta assembleia/i.test(d.mensagem || ''));

module.exports = { prazoEncerrado, extrairProtocolo, parcelasEmAtraso, jaCredenciado };
