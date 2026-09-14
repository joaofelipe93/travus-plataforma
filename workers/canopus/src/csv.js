'use strict';

const fs = require('fs');
const { parse } = require('csv-parse/sync');

// "CONTRATAÇÃO " → "contratacao": cabeçalhos em qualquer caixa, com ou sem acento.
const normalizeHeader = (h) =>
  h.normalize('NFD').replace(/[̀-ͯ]/g, '').trim().toLowerCase();

/**
 * Lê o CSV de cotas. Colunas obrigatórias: grupo, cota. Opcionais: versao (padrão 00)
 * e nome (usado no nome do PDF). Outras colunas são ignoradas, então a planilha de
 * clientes (cotasreal.csv: NOME, GRUPO, COTA, TELEFONE...) pode ser usada direto.
 * Aceita separador vírgula ou ponto-e-vírgula.
 */
function readCotas(csvPath) {
  const raw = fs.readFileSync(csvPath, 'utf8');
  const delimiter = raw.split('\n')[0].includes(';') ? ';' : ',';
  const rows = parse(raw, {
    columns: (headers) => headers.map(normalizeHeader),
    skip_empty_lines: true,
    relax_column_count: true,
    trim: true,
    delimiter,
    bom: true,
  });

  if (rows.length && !('grupo' in rows[0] && 'cota' in rows[0])) {
    throw new Error(`CSV sem as colunas "grupo" e "cota" (cabeçalho lido: ${Object.keys(rows[0]).join(', ')})`);
  }

  const out = [];
  rows.forEach((row, i) => {
    const grupo = String(row.grupo || '').replace(/\D/g, '').padStart(6, '0');
    const cota = String(row.cota || '').replace(/\D/g, '').padStart(4, '0');
    const versao = String(row.versao || '00').replace(/\D/g, '').padStart(2, '0');
    const nome = String(row.nome || '').trim();
    if (!grupo || grupo === '000000' || !cota || cota === '0000') {
      throw new Error(`Linha ${i + 2} do CSV inválida: grupo=${row.grupo} cota=${row.cota}`);
    }
    out.push({ grupo, cota, versao, nome, lineNumber: i + 2 });
  });

  if (out.length === 0) throw new Error('CSV vazio (nenhuma cota para processar).');
  return out;
}

module.exports = { readCotas };
