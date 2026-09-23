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
};

export type ClienteResumo = {
  id: number;
  nome: string;
  telefone: string | null;
  email: string | null;
  total_cotas: number;
  cotas_ativas: number;
};

export type Cliente = {
  id: number;
  nome: string;
  telefone: string | null;
  email: string | null;
  origem: string;
  criado_em: string;
  atualizado_em: string;
};

export type TipoExecucao = "dry_run" | "real" | "reimpressao";

export type StatusExecucao = "na_fila" | "em_andamento" | "concluida" | "concluida_com_erros" | "cancelada" | "falhou";

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
