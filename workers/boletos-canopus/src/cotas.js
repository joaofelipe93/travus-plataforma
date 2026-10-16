'use strict';

/**
 * Casamento entre as cotas que o Boleto Fácil devolve e as do cadastro da plataforma.
 *
 * O site e o cadastro escrevem a mesma cota de formas diferentes — isso foi medido numa
 * resposta real em 16/10/2026 (ver docs/canopus-boleto-facil.md):
 *   site:     { CD_Grupo: "006650" (texto, 6 dígitos), CD_Cota: 924 (NÚMERO, sem zeros),
 *               Versao: 0 (número), ID_Cota: 468379 }
 *   cadastro: { grupo: "006650", cota: "0924", versao: "00" }
 * Comparar como texto erraria em toda cota de menos de 4 dígitos. Então os dois lados são
 * normalizados com zeros à esquerda (6/4/2) antes de comparar.
 */

/** Só os dígitos, com zeros à esquerda até a largura. Devolve null se não houver dígito. */
function normalizar(valor, largura) {
  const digitos = String(valor ?? '').replace(/\D/g, '');
  if (digitos === '') return null;
  if (digitos.length > largura) return digitos; // não cabe: devolve como está, não casa
  return digitos.padStart(largura, '0');
}

/** Chave de comparação de uma cota. versao null = o site não informou. */
function chave(grupo, cota, versao) {
  const g = normalizar(grupo, 6);
  const c = normalizar(cota, 4);
  if (!g || !c) return null;
  const v = versao === null || versao === undefined || versao === '' ? null : normalizar(versao, 2);
  return { grupo: g, cota: c, versao: v, tag: v === null ? `${g}-${c}` : `${g}-${c}-${v}` };
}

/** Uma cota como o site devolve, normalizada. idCota é o que o generate-bill precisa. */
function cotaDoSite(bruta) {
  const k = chave(bruta && bruta.CD_Grupo, bruta && bruta.CD_Cota, bruta && bruta.Versao);
  if (!k) return null;
  const idCota = String((bruta && bruta.ID_Cota) ?? '').trim();
  if (!/^\d+$/.test(idCota)) return null;
  return { ...k, idCota, bruta };
}

/**
 * Casa a lista do site com as cotas cadastradas do cliente.
 * Devolve { casadas: [{ site, cadastro }], soNoSite: [], soNoCadastro: [] }.
 *
 * Quando o site não informa a versão, casa por grupo+cota — e só se houver UMA cota cadastrada
 * com aquele grupo e cota. Duas versões cadastradas e nenhuma versão no site é ambiguidade:
 * ficam em soNoSite, para virar aviso em vez de boleto trocado.
 */
function casarCotas(doSite, doCadastro) {
  const normalizadasDoSite = [];
  const invalidas = [];
  for (const bruta of doSite || []) {
    const c = cotaDoSite(bruta);
    if (c) normalizadasDoSite.push(c);
    else invalidas.push(bruta);
  }

  const cadastro = (doCadastro || [])
    .map((q) => ({ ...chave(q.grupo, q.cota, q.versao), cadastro: q }))
    .filter((q) => q.grupo && q.cota);

  const usadas = new Set();
  const casadas = [];
  const soNoSite = [];

  for (const site of normalizadasDoSite) {
    const candidatos = cadastro.filter(
      (q) => q.grupo === site.grupo && q.cota === site.cota && (site.versao === null || q.versao === site.versao),
    );
    if (candidatos.length === 1) {
      casadas.push({ site, cadastro: candidatos[0].cadastro });
      usadas.add(candidatos[0].cadastro.id);
    } else {
      soNoSite.push({ site, ambiguo: candidatos.length > 1 });
    }
  }

  const soNoCadastro = cadastro.filter((q) => !usadas.has(q.cadastro.id)).map((q) => q.cadastro);
  return { casadas, soNoSite, soNoCadastro, invalidas };
}

module.exports = { normalizar, chave, cotaDoSite, casarCotas };
