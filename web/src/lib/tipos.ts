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

export type Totais = {
  linhas: number;
  clientes: number;
  clientes_novos: number;
  clientes_alterados: number;
  cotas_novas: number;
  cotas_alteradas: number;
  cotas_sem_mudanca: number;
  fora_da_planilha: number;
  erros: number;
};

export type Mudanca = { campo: string; antes: string; depois: string };

export type CotaPrevia = {
  linha?: number;
  administradora: string;
  grupo: string;
  cota: string;
  versao: string;
  cliente: string;
  tipo_consorcio?: string;
  ativa?: boolean;
  mudancas?: Mudanca[];
};

export type ClientePrevia = {
  nome: string;
  telefone?: string;
  email?: string;
  cotas: number;
  mudancas?: Mudanca[];
};

export type Previa = {
  totais: Totais;
  erros: { linha: number; mensagem: string }[];
  clientes_novos: ClientePrevia[];
  clientes_alterados: ClientePrevia[];
  cotas_novas: CotaPrevia[];
  cotas_alteradas: CotaPrevia[];
  fora_da_planilha: CotaPrevia[];
};

export type StatusImportacao = "previa" | "aplicada" | "descartada";

export type Importacao = {
  id: number;
  status: StatusImportacao;
  arquivo_nome: string;
  criada_por_nome: string;
  criada_em: string;
  finalizada_em: string | null;
  previa: Previa;
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

export type ImportacaoResumo = {
  id: number;
  status: StatusImportacao;
  arquivo_nome: string;
  criada_por_nome: string;
  totais: Totais | null;
  criada_em: string;
  finalizada_em: string | null;
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
