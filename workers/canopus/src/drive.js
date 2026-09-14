'use strict';

const fs = require('fs');
const path = require('path');
const readline = require('readline');
const http = require('http');
const { URL } = require('url');
const { google } = require('googleapis');

const SCOPES = ['https://www.googleapis.com/auth/drive.file'];

/**
 * Google Drive uploader com OAuth "installed app".
 *
 * Fluxo de auth (primeira execução):
 *   - Lê o Client ID/Secret de aplicativo Desktop (GOOGLE_CLIENT_ID/GOOGLE_CLIENT_SECRET
 *     no .env, ou credentials.json), criado no Google Cloud Console — passo a passo no README.md.
 *   - Abre um servidor HTTP local em porta aleatória e imprime a URL
 *     que o usuário deve abrir no navegador.
 *   - Recebe o "code" no redirect, troca por access + refresh token.
 *   - Salva em token.json. Nas próximas execuções, só recarrega o arquivo.
 */
class DriveClient {
  constructor({ config, logger }) {
    this.config = config;
    this.logger = logger;
    this.oauth = null;
    this.drive = null;
  }

  async init() {
    const { clientId, clientSecret } = this._loadClientKeys();

    // Deixamos o redirect_uri em branco aqui — resolveremos com um servidor local.
    this.oauth = new google.auth.OAuth2(clientId, clientSecret, 'http://127.0.0.1');

    if (fs.existsSync(this.config.drive.tokenPath)) {
      const token = JSON.parse(fs.readFileSync(this.config.drive.tokenPath, 'utf8'));
      this.oauth.setCredentials(token);
      this.logger.ok('drive: token OAuth carregado do disco');
    } else {
      await this._interactiveAuth();
    }

    // Salva atualizações de token (refresh) automaticamente.
    this.oauth.on('tokens', (tokens) => {
      try {
        const current = fs.existsSync(this.config.drive.tokenPath)
          ? JSON.parse(fs.readFileSync(this.config.drive.tokenPath, 'utf8'))
          : {};
        fs.writeFileSync(this.config.drive.tokenPath, JSON.stringify({ ...current, ...tokens }, null, 2));
      } catch (e) {
        this.logger.warn('drive: falha ao persistir token atualizado', { err: e.message });
      }
    });

    this.drive = google.drive({ version: 'v3', auth: this.oauth });
  }

  /**
   * Client ID/Secret do OAuth: vêm do .env (GOOGLE_CLIENT_ID + GOOGLE_CLIENT_SECRET)
   * ou, se ambos estiverem vazios, do credentials.json.
   */
  _loadClientKeys() {
    const { clientId, clientSecret, credentialsPath } = this.config.drive;
    if (clientId || clientSecret) {
      if (!clientId || !clientSecret) {
        throw new Error('Defina GOOGLE_CLIENT_ID e GOOGLE_CLIENT_SECRET juntos no .env.');
      }
      return { clientId, clientSecret };
    }

    if (!fs.existsSync(credentialsPath)) {
      throw new Error(
        'Credenciais do Google ausentes. Defina GOOGLE_CLIENT_ID e GOOGLE_CLIENT_SECRET no .env ' +
          `(ou salve o credentials.json em ${credentialsPath}). Veja README.md.`
      );
    }
    const raw = JSON.parse(fs.readFileSync(credentialsPath, 'utf8'));
    const key = raw.installed || raw.web;
    if (!key) throw new Error('credentials.json inválido: esperado bloco "installed" (Desktop app).');
    return { clientId: key.client_id, clientSecret: key.client_secret };
  }

  async _interactiveAuth() {
    // Servidor local para receber o redirect com ?code=...
    // Se DRIVE_OAUTH_PORT for definido, usa essa porta fixa (necessário no Docker).
    // Se BIND_HOST for definido (ex: 0.0.0.0 no Docker), o servidor aceita conexões externas ao container.
    const fixedPort = Number(process.env.DRIVE_OAUTH_PORT || 0) || 0;
    const bindHost = process.env.BIND_HOST || '127.0.0.1';
    const server = http.createServer();
    await new Promise((resolve, reject) => {
      server.once('error', reject);
      server.listen(fixedPort, bindHost, resolve);
    });
    const port = server.address().port;
    const redirectUri = `http://127.0.0.1:${port}/oauth2callback`;
    this.oauth.redirectUri = redirectUri;

    const authUrl = this.oauth.generateAuthUrl({
      access_type: 'offline',
      prompt: 'consent',
      scope: SCOPES,
    });

    console.log('\n=== Autorização Google Drive necessária ===');
    console.log('Abra a URL abaixo no seu navegador (com a conta certa logada) e autorize o acesso:');
    console.log(authUrl);
    console.log('===========================================\n');

    const code = await new Promise((resolve, reject) => {
      server.on('request', async (req, res) => {
        try {
          const url = new URL(req.url, `http://127.0.0.1:${port}`);
          const c = url.searchParams.get('code');
          const err = url.searchParams.get('error');
          if (err) {
            res.end(`Erro: ${err}`);
            reject(new Error(`OAuth negado: ${err}`));
            return;
          }
          if (!c) {
            res.end('Sem code — feche essa aba.');
            return;
          }
          res.end('Autorização concluída! Você pode fechar esta aba e voltar ao terminal.');
          resolve(c);
        } catch (e) { reject(e); }
      });
    });
    server.close();

    const { tokens } = await this.oauth.getToken(code);
    this.oauth.setCredentials(tokens);
    fs.writeFileSync(this.config.drive.tokenPath, JSON.stringify(tokens, null, 2));
    this.logger.ok('drive: token salvo em ' + this.config.drive.tokenPath);
  }

  async uploadPdf(localPath, remoteFileName) {
    const name = remoteFileName || path.basename(localPath);
    const res = await this.drive.files.create({
      requestBody: {
        name,
        parents: [this.config.drive.folderId],
        mimeType: 'application/pdf',
      },
      media: {
        mimeType: 'application/pdf',
        body: fs.createReadStream(localPath),
      },
      fields: 'id, name, webViewLink',
    });
    this.logger.ok('drive: upload concluído', {
      id: res.data.id,
      name: res.data.name,
      link: res.data.webViewLink,
    });
    return res.data;
  }
}

module.exports = { DriveClient };
