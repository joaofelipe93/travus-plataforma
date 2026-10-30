// Formatos das respostas da API (api/internal/httpapi).

export type Perfil = "admin" | "operador" | "leitura";

export type Usuario = {
  id: number;
  email: string;
  nome: string;
  perfil: Perfil;
};

export type Sessao = {
  usuario: Usuario;
  csrf_token: string;
};

export type Cota = {
  id: number;
  cliente_id: number;
  cliente_nome: string;
  administradora: string;
  grupo: string;
  cota: string;
  versao: string;
  tipo_consorcio: string | null;
  modalidade_padrao: string;
  ativa: boolean;
  atualizado_em: string;
  // Campos do cadastro (o que a planilha antiga trazia solto). Contratação em AAAA-MM-DD.
  vendedor: string | null;
  forma_pagamento: string | null;
  vencimento_parcela: number | null;
  dia_assembleia: number | null;
  contratacao: string | null;
};

// O que o formulário do CRM manda para /clientes e /cotas: texto puro, a API normaliza
// (zeros à esquerda no grupo/cota/versão, maiúsculas, campo em branco vira nulo).
export type CotaFormulario = {
  administradora: string;
  grupo: string;
  cota: string;
  versao: string;
  tipo_consorcio: string;
  modalidade_padrao: string;
  ativa: boolean;
  vendedor: string;
  forma_pagamento: string;
  vencimento_parcela: number | null;
  dia_assembleia: number | null;
  contratacao: string;
};

export type ClienteFormulario = {
  nome: string;
  telefone: string;
  email: string;
  // CPF ou CNPJ como a pessoa digitar: a API guarda só os dígitos e confere o verificador.
  cpf_cnpj: string;
};

export type ClienteResumo = {
  id: number;
  nome: string;
  telefone: string | null;
  email: string | null;
  // Só dígitos (11 ou 14); sem documento é null. É o que o Boleto Fácil do Canopus pede.
  cpf_cnpj: string | null;
  total_cotas: number;
  cotas_ativas: number;
  // Ids dos serviços em que o cliente está ativo ("lances", "boletos").
  servicos: string[];
};

// Os serviços que agem sobre um cliente (GET /servicos). A lista é a de api/internal/servicos.
export type ServicoCatalogo = {
  id: string;
  nome: string;
  descricao: string;
};

// Um serviço do catálogo com a situação deste cliente nele. A API manda sempre o catálogo
// inteiro, então a tela desenha um interruptor por serviço, ligado ou não.
export type ServicoDoCliente = ServicoCatalogo & {
  ativo: boolean;
  atualizado_em: string | null;
  alterado_por_nome: string | null;
};

export type Cliente = {
  id: number;
  nome: string;
  telefone: string | null;
  email: string | null;
  cpf_cnpj: string | null;
  origem: string;
  criado_em: string;
  atualizado_em: string;
};

export type TipoExecucao = "dry_run" | "real" | "reimpressao";

export type StatusExecucao = "na_fila" | "em_andamento" | "concluida" | "concluida_com_erros" | "cancelada" | "falhou";

// Quem pediu a execução: a tela ou o dry-run automático do mês (Canopus → Configurações).
export type OrigemExecucao = "manual" | "agendada";

export type StatusCotaExecucao =
  | "pendente"
  | "em_andamento"
  | "verificada"
  | "erro_antes_confirmar"
  | "confirmacao_iniciada"
  | "confirmada"
  | "erro_apos_confirmar"
  | "reimpressa"
  | "cancelada";

export type ExecucaoResumo = {
  id: number;
  tipo: TipoExecucao;
  status: StatusExecucao;
  origem: OrigemExecucao;
  criada_por_nome: string;
  criada_em: string;
  iniciada_em: string | null;
  finalizada_em: string | null;
  cancelamento_solicitado: boolean;
  erro: string | null;
  dry_run_origem_id: number | null;
  total: number;
  sucesso: number;
  com_erro: number;
  restantes: number;
};

export type LanceHistorico = {
  protocolo: string;
  assembleia: string;
  credenciamento?: string | null;
  modalidade?: string | null;
  percentual?: string | null;
};

// Lido da tela de credenciamento e do Histórico pelo worker (só leitura).
export type DetalhesCota = {
  assembleia_data?: string | null;
  assembleia_numero?: string | null;
  percentual_segundo_fixo?: string | null;
  // Modalidade marcada no Newcon e o percentual dela (cota de lance Fixo não usa o do 2º Fixo).
  modalidade_lance?: string | null;
  percentual_lance?: string | null;
  ultimo_lance?: string | null;
  historico_lido?: boolean;
  lances_no_historico?: number;
  lances_nesta_assembleia?: number;
  lances?: LanceHistorico[];
};

export type CotaExecucao = {
  id: number;
  cota_id: number;
  ordem: number;
  grupo: string;
  cota: string;
  versao: string;
  cliente_nome: string;
  modalidade: string;
  status: StatusCotaExecucao;
  erro_tipo: "conhecido" | "inesperado" | null;
  erro: string | null;
  detalhes: DetalhesCota;
  screenshot_id: string | null;
  tentativas: number;
  iniciada_em: string | null;
  finalizada_em: string | null;
  assembleia_aprovada: string | null;
  permitir_lance_existente: boolean;
  protocolo: string | null;
};

export type Execucao = {
  id: number;
  tipo: TipoExecucao;
  status: StatusExecucao;
  origem: OrigemExecucao;
  criada_por_nome: string;
  aprovada_por_nome: string | null;
  dry_run_origem_id: number | null;
  criada_em: string;
  iniciada_em: string | null;
  finalizada_em: string | null;
  cancelamento_solicitado: boolean;
  cancelada_por_nome: string | null;
  erro: string | null;
  posicao_fila: number | null;
};

export type StatusDrive = "pendente" | "enviando" | "enviado" | "erro" | "sem_pdf" | "nao_enviar";

export type LanceExecucao = {
  id: number;
  execucao_cota_id: number;
  protocolo: string;
  pdf_id: string | null;
  drive_status: StatusDrive;
  drive_link: string | null;
  drive_erro: string | null;
  parcelas_em_atraso: boolean;
};

export type DetalheExecucao = {
  execucao: Execucao;
  totais: Partial<Record<StatusCotaExecucao, number>>;
  cotas: CotaExecucao[];
  lances: LanceExecucao[];
};

export type EventoExecucao = {
  id: number;
  execucao_cota_id: number | null;
  nivel: "info" | "ok" | "aviso" | "erro";
  mensagem: string;
  dados: Record<string, unknown>;
  criado_em: string;
};

export type LanceCliente = {
  id: number;
  cota_id: number;
  grupo: string;
  cota: string;
  versao: string;
  origem: "plataforma" | "historico";
  protocolo: string;
  assembleia_data: string | null;
  assembleia_numero: string | null;
  modalidade: string;
  percentual: string | null;
  parcelas_em_atraso: boolean;
  lance_existente_autorizado: boolean;
  pdf_id: string | null;
  drive_status: StatusDrive;
  drive_link: string | null;
  drive_erro: string | null;
  registrado_em: string | null;
  execucao_id: number | null;
};

export type CotaRevisao = {
  execucao_cota_id: number;
  cota_id: number;
  grupo: string;
  cota: string;
  versao: string;
  cliente_nome: string;
  ativa: boolean;
  assembleia_data: string | null;
  assembleia_numero: string | null;
  percentual_segundo_fixo: string | null;
  percentual_lance: string | null;
  // Modalidade cadastrada na cota: é a que o worker vai marcar.
  modalidade: string;
  historico_lido: boolean;
  lances_nesta_assembleia: number;
  lances: LanceHistorico[];
  lances_plataforma: string[];
  exige_autorizacao: boolean;
  screenshot_id: string | null;
  bloqueio: string;
};

export type Revisao = {
  dry_run: {
    id: number;
    status: StatusExecucao;
    finalizada_em: string | null;
    valido_ate: string | null;
    expirado: boolean;
    execucao_real_id: number | null;
  };
  lance_real_habilitado: boolean;
  pode_aprovar: boolean;
  bloqueio: string;
  cotas: CotaRevisao[];
};

export type EstadoWhatsapp = "conectado" | "aguardando_qr" | "conectando" | "desconectado" | "indisponivel";

export type GrupoDestinoWhatsapp = {
  jid: string;
  nome: string | null;
  // tela: escolhido pelo admin; ambiente: CHECKIN_WHATSAPP_GROUP_JID enquanto ninguém escolheu.
  origem: "tela" | "ambiente";
};

export type SituacaoWhatsapp = {
  disponivel: boolean;
  motivo?: string;
  estado: EstadoWhatsapp;
  numero: string | null;
  qr: string | null;
  grupo: GrupoDestinoWhatsapp | null;
  fila: Partial<Record<"pendente" | "enviada" | "falhou", number>>;
};

export type GrupoWhatsapp = {
  jid: string;
  nome: string;
  participantes: number;
};

// Meu perfil (api/internal/httpapi/perfil.go). O valor de uma credencial nunca vem da API:
// só a dica (últimos caracteres) e quando foi cadastrada.
export type MeuPerfil = {
  id: number;
  email: string;
  nome: string;
  perfil: Perfil;
  telefone: string | null;
  cargo: string | null;
  observacoes: string | null;
  criado_em: string;
  atualizado_em: string;
};

export type CampoCredencial = {
  id: string;
  rotulo: string;
  ajuda?: string;
  obrigatorio: boolean;
};

export type Credencial = {
  id: string;
  nome: string;
  descricao: string;
  como_obter: string;
  link_ajuda?: string;
  campos: CampoCredencial[];
  cadastrada: boolean;
  dica?: string;
  atualizada_em: string | null;
};

export type RespostaPerfil = {
  usuario: MeuPerfil;
  credenciais: Credencial[];
  // Sem CHAVE_CRIPTOGRAFIA na API não dá para guardar credencial: a tela avisa.
  cofre_configurado: boolean;
};

// Reservas recebidas pelo notificador de check-in (GET /reservas). A reserva e o cancelamento
// dela vêm juntos num item. Datas da estadia em AAAA-MM-DD.
// "resumo": sem aviso próprio, a reserva vai no resumo diário do grupo (08h e 17h).
export type MensagemReserva = "enviada" | "pendente" | "falhou" | "resumo" | "sem_mensagem";

export type Reserva = {
  chave: string;
  situacao: "confirmada" | "cancelada";
  status: string | null;
  hospede: string | null;
  telefone: string | null;
  email: string | null;
  imovel: string | null;
  canal: string | null;
  check_in: string | null;
  check_out: string | null;
  noites: number | null;
  hospedes: number | null;
  valor_centavos: number | null;
  motivo_cancelamento: string | null;
  link_precheckin: string | null;
  recebida_em: string;
  atualizada_em: string;
  cancelada_em: string | null;
  // Aviso no grupo do WhatsApp.
  mensagem: MensagemReserva;
  mensagem_cancelamento: MensagemReserva | null;
  // false: nenhum campo conhecido no payload (formato novo do PMS).
  reconhecida: boolean;
};

export type RespostaReservas = {
  reservas: Reserva[];
  limite_atingido: boolean;
};

// Configuração do serviço Canopus (tela Canopus → Configurações; GET/PUT /configuracao/canopus).
// drive_pasta_id é o que está gravado; drive_pasta_em_vigor pode vir do ambiente.
export type ConfiguracaoCanopus = {
  dry_run_automatico: boolean;
  dry_run_dia: number;
  dry_run_hora: number;
  dry_run_minuto: number;
  aviso_email: boolean;
  drive_pasta_id: string | null;
  drive_pasta_em_vigor: string;
  drive_pasta_origem: "configuracao" | "ambiente" | "nenhuma";
  validade_dry_run_minutos: number;
  retencao_screenshots_dias: number;
  // Lance real automático: ligado, o dry-run automático vira lance de verdade sem aprovação.
  lance_real_automatico: boolean;
  lance_real_max_cotas: number;
  lance_real_espera_minutos: number;
  lance_real_automatico_por_nome: string | null;
  atualizado_por_nome: string | null;
  atualizado_em: string;
  proximo_dry_run: string;
  ultimo_disparo_em: string | null;
  ultimo_disparo_resultado: ResultadoDisparo | null;
};

export type ResultadoDisparo = "criada" | "sem_cotas" | "sem_responsavel" | "atrasada" | "antes_de_ligar";

export type RespostaConfiguracaoCanopus = {
  configuracao: ConfiguracaoCanopus;
  pode_editar: boolean;
  // Só admin liga o lance real automático (e muda o teto e a espera).
  pode_editar_lance_real: boolean;
  email_configurado: boolean;
  // Chave LANCE_REAL_HABILITADO do servidor: desligada, nenhum lance real acontece.
  lance_real_habilitado: boolean;
};

// Conta do Google que recebe os comprovantes de lance (GET /integracoes/google-drive, só admin).
// O token fica cifrado no banco e é importado no servidor (make google-token): aqui só se sabe
// se ele existe e de quando é.
export type SituacaoGoogleDrive = {
  lance_real_habilitado: boolean;
  // Sem CHAVE_CRIPTOGRAFIA na API o token não decifra: não há envio ao Drive.
  cofre_configurado: boolean;
  client_configurado: boolean;
  pasta_configurada: boolean;
  pasta_origem: "configuracao" | "ambiente" | "nenhuma";
  token_importado: boolean;
  token_atualizado_em: string | null;
};


// ===== Serviço de boletos da Canopus (issue #48) =====

// Uma rodada é uma passada pelo cadastro: 'envio' busca os boletos do mês e manda; 'cobranca'
// pergunta de novo depois e cobra quem não pagou.
export type TipoRodada = "envio" | "cobranca";

export type StatusRodada =
  | "na_fila"
  | "em_andamento"
  | "concluida"
  | "concluida_com_erros"
  | "cancelada"
  | "falhou";

export type RodadaResumo = {
  id: number;
  tipo: TipoRodada;
  // Mês de referência, "AAAA-MM".
  referencia: string;
  status: StatusRodada;
  origem: OrigemExecucao;
  criada_por_nome: string;
  criada_em: string;
  finalizada_em: string | null;
  erro: string | null;
  total: number;
  consultados: number;
  sem_boleto: number;
  com_erro: number;
  parcelas: number;
};

export type Rodada = {
  id: number;
  tipo: TipoRodada;
  referencia: string;
  status: StatusRodada;
  origem: OrigemExecucao;
  criada_por_nome: string;
  cancelada_por_nome: string | null;
  cancelamento_solicitado: boolean;
  erro: string | null;
  criada_em: string;
  iniciada_em: string | null;
  finalizada_em: string | null;
};

export type StatusClienteRodada =
  | "pendente"
  | "em_andamento"
  | "consultado"
  | "sem_boleto"
  | "erro"
  | "cancelado";

export type ClienteRodada = {
  id: number;
  cliente_id: number;
  ordem: number;
  cliente_nome: string;
  status: StatusClienteRodada;
  erro_tipo: "conhecido" | "inesperado" | null;
  erro: string | null;
  // Cotas que o CPF tem na Canopus e quantas delas estão no cadastro.
  cotas_no_site: number;
  cotas_casadas: number;
  tentativas: number;
  iniciado_em: string | null;
  finalizado_em: string | null;
};

export type ParcelaBoleto = {
  id: number;
  rodada_cliente_id: number;
  cota_id: number;
  grupo: string;
  cota: string;
  versao: string;
  // Texto: vem com zeros à esquerda e pode ser "DIF" (reajuste).
  parcela: string;
  vencimento: string;
  valor_centavos: number;
  tipo: string;
  pdf_id: string | null;
  criada_em: string;
};

export type StatusMensagemBoleto = "pendente" | "enviada" | "falhou" | "cancelada";

export type MensagemBoleto = {
  id: number;
  rodada_cliente_id: number | null;
  tipo: "envio" | "cobranca" | "teste";
  parcela_id: number | null;
  destino_nome: string;
  texto: string;
  pdf_id: string | null;
  status: StatusMensagemBoleto;
  tentativas: number;
  // Em ensaio, traz o motivo de a mensagem não ter sido enviada.
  ultimo_erro: string | null;
  enviada_em: string | null;
  criada_em: string;
};

export type EventoRodada = {
  id: number;
  rodada_cliente_id: number | null;
  nivel: "info" | "ok" | "aviso" | "erro";
  mensagem: string;
  criado_em: string;
};

export type DetalheRodada = {
  rodada: Rodada;
  clientes: ClienteRodada[];
  parcelas: ParcelaBoleto[];
  eventos: EventoRodada[];
  mensagens: MensagemBoleto[];
  // Só na cobrança: quantas parcelas do envio do mês já foram pagas.
  cobranca?: { enviadas: number; pagas: number };
};

export type ConfiguracaoBoletos = {
  agendamento_ativo: boolean;
  dia_envio: number;
  hora_envio: number;
  dia_cobranca: number;
  hora_cobranca: number;
  // Dia que cai em sábado, domingo ou feriado antecipa para o dia útil anterior (issue #48).
  antecipar_dia_util: boolean;
  envio_habilitado: boolean;
  pausa_envio_segundos: number;
  max_mensagens_rodada: number;
  // Modelo das mensagens: a mesma para todos, com os campos entre chaves preenchidos no envio.
  texto_envio: string;
  texto_cobranca: string;
  envio_habilitado_por_nome: string;
  atualizado_por_nome: string;
  atualizado_em: string;
  proximo_envio: string;
  proxima_cobranca: string;
  // "o dia 14 cai no sábado", quando houve antecipação.
  motivo_envio: string;
  motivo_cobranca: string;
};

export type RespostaConfiguracaoBoletos = {
  configuracao: ConfiguracaoBoletos;
  pode_editar: boolean;
  // Só admin liga o envio no WhatsApp dos clientes.
  pode_ligar_envio: boolean;
  // Chave BOLETOS_ENVIO_HABILITADO do servidor: desligada, nada sai.
  envio_habilitado_servidor: boolean;
  fila: { pendentes: number; falharam: number; proxima: string | null };
  // O que a tela precisa para a ajuda dos campos, a prévia e o "voltar ao texto padrão".
  mensagens: {
    campos_envio: CampoMensagem[];
    campos_cobranca: CampoMensagem[];
    padrao_envio: string;
    padrao_cobranca: string;
    max_caracteres: number;
  };
};

// Um campo que a plataforma preenche no texto ({grupo}, {valor}…).
export type CampoMensagem = {
  nome: string;
  descricao: string;
  exemplo: string;
};

export type SituacaoWhatsappBoletos = {
  disponivel: boolean;
  motivo?: string;
  estado: EstadoWhatsapp;
  numero: string | null;
  qr: string | null;
  envio_habilitado_servico: boolean;
  envio_habilitado_configuracao: boolean;
  envio_habilitado_servidor: boolean;
  pausa_envio_segundos: number;
  fila: Partial<Record<StatusMensagemBoleto, number>>;
};

// ---------------------------------------------------------------------------
// Atendimento (bots de WhatsApp, issue #100)
// ---------------------------------------------------------------------------

export type ModeloBot = {
  id: string;
  nome: string;
  descricao: string;
};

// Uma pergunta do atendimento. O bot faz uma por vez, na ordem, com as palavras dele — o texto
// aqui é o que ele precisa descobrir, não a frase que vai enviar.
export type PerguntaBot = {
  id: number;
  ordem: number;
  texto: string;
  // Onde a resposta é guardada: vira campo do lead.
  campo: string;
  orientacao: string | null;
  obrigatoria: boolean;
};

// Um bot pré-escrito que a tela oferece em "Novo bot" (a Travus tem vários segmentos, então é
// catálogo, não padrão).
export type PontoDePartida = {
  id: string;
  nome: string;
  descricao: string;
  instrucoes: string;
  saudacao: string;
  perguntas: { texto: string; campo: string; orientacao: string | null; obrigatoria: boolean }[];
};

export type Bot = {
  id: number;
  nome: string;
  slug: string;
  modelo: string;
  instrucoes: string;
  saudacao: string | null;
  ativo: boolean;
  max_mensagens_hora: number;
  max_tokens_conversa: number;
  pausa_resposta_segundos: number;
  criado_em: string;
  atualizado_em: string;
  // Só na lista.
  perguntas?: number;
  conversas_abertas?: number;
};

export type DetalheBot = {
  bot: Bot;
  perguntas: PerguntaBot[];
  conversas: { total: number; abertas: number };
};

export type StatusConversa = "aberta" | "encerrada" | "abandonada" | "erro";
export type StatusMensagemBot = "recebida" | "pendente" | "enviada" | "falhou" | "cancelada";

export type ConversaResumo = {
  id: number;
  bot_id: number;
  bot_nome: string;
  bot_slug: string;
  telefone: string;
  contato_nome: string | null;
  status: StatusConversa;
  motivo: string | null;
  mensagens: number;
  dados: number;
  ultimo_texto: string;
  ultima_direcao: string;
  tokens: number;
  iniciada_em: string;
  ultima_em: string;
  encerrada_em: string | null;
};

export type MensagemBot = {
  id: number;
  direcao: "recebida" | "enviada";
  texto: string;
  status: StatusMensagemBot;
  motivo: string | null;
  tentativas: number;
  ultimo_erro: string | null;
  criada_em: string;
  enviada_em: string | null;
};

export type DadoColetado = {
  campo: string;
  valor: string;
  // A pergunta que originou a resposta; nula quando a pessoa contou sem ser perguntada.
  pergunta: string | null;
  criado_em: string;
  atualizado_em: string;
};

export type DetalheConversa = {
  // O lead que esta conversa originou (etapa E), quando existe.
  lead: { id: number; status: StatusLead; cliente_id: number | null } | null;
  conversa: {
    id: number;
    bot_id: number;
    bot_nome: string;
    bot_slug: string;
    telefone: string;
    contato_nome: string | null;
    status: StatusConversa;
    motivo: string | null;
    perguntas_do_bot: number;
    respostas: number;
    tokens_entrada: number;
    tokens_saida: number;
    iniciada_em: string;
    ultima_em: string;
    encerrada_em: string | null;
  };
  mensagens: MensagemBot[];
  dados: DadoColetado[];
};

export type ResumoAtendimento = {
  bots: { total: number; ativos: number };
  conversas: { abertas: number; hoje: number; concluidas_mes: number; pediram_humano_mes: number };
  fila: { pendentes: number; falharam: number; canceladas_mes: number; proxima: string | null };
  leads: { novos: number; aproveitados: number; descartados: number; no_mes: number };
  dados_coletados_mes: number;
  tokens_mes: number;
  modelo_disponivel: boolean;
};

// Um bot na tela de Conexões: o cadastro + o estado da sessão de WhatsApp dele no worker.
export type BotNaConexao = {
  id: number;
  nome: string;
  slug: string;
  ativo: boolean;
  estado: EstadoWhatsapp;
  // O worker está no ar e não tem sessão para este bot (bot desligado, ou acima do teto).
  sem_sessao: boolean;
  conectado: boolean;
  numero: string | null;
  qr: string | null;
  aberta_ha_ms: number | null;
  ultimo_erro: string | null;
  fila: Partial<Record<StatusMensagemBot, number>>;
};

export type SituacaoWhatsappAtendimento = {
  // O worker respondeu? Sem ele, nenhum bot conversa — e isso não é erro da tela.
  disponivel: boolean;
  motivo?: string;
  modelo_disponivel: boolean;
  bots: BotNaConexao[];
};

// Um lead: a conversa que colheu algo, esperando alguém revisar e promover a cliente.
export type StatusLead = "novo" | "aproveitado" | "descartado";

export type Lead = {
  id: number;
  conversa_id: number;
  bot_id: number;
  bot_nome: string;
  bot_slug: string;
  status: StatusLead;
  nome: string | null;
  telefone: string | null;
  email: string | null;
  observacoes: string | null;
  // Tudo o que o bot colheu, inclusive o que não tem campo próprio aqui.
  dados: Record<string, string>;
  contato_nome: string | null;
  conversa_telefone: string;
  conversa_status: StatusConversa;
  conversa_motivo: string | null;
  mensagens: number;
  ultima_em: string;
  cliente_id: number | null;
  cliente_nome: string | null;
  motivo_descarte: string | null;
  revisado_por_nome: string | null;
  revisado_em: string | null;
  criado_em: string;
};

export type RespostaLeads = {
  leads: Lead[];
  contagem: { novos: number; aproveitados: number; descartados: number; no_mes: number };
};
