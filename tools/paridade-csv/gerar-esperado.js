#!/usr/bin/env node
'use strict';

// Gera o resultado esperado do teste de paridade do importador (Go) rodando o csv.js
// ORIGINAL do script legado sobre as planilhas fictícias de
// api/internal/importacao/testdata/paridade/*.csv.
//
// Uso: make paridade-csv   (precisa de `npm ci` em workers/canopus)

const fs = require('fs');
const path = require('path');

const raiz = path.resolve(__dirname, '../..');
const { readCotas } = require(path.join(raiz, 'workers/canopus/src/csv.js'));
const dir = path.join(raiz, 'api/internal/importacao/testdata/paridade');

for (const arquivo of fs.readdirSync(dir).filter((f) => f.endsWith('.csv')).sort()) {
  let saida;
  try {
    saida = { cotas: readCotas(path.join(dir, arquivo)) };
  } catch (e) {
    saida = { erro: e.message };
  }
  fs.writeFileSync(path.join(dir, arquivo.replace(/\.csv$/, '.esperado.json')), JSON.stringify(saida, null, 2) + '\n');
  console.log(`${arquivo}: ${saida.erro ? `erro: ${saida.erro}` : `${saida.cotas.length} cota(s)`}`);
}
