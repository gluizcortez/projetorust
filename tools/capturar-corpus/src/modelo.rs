//! Tipos de domínio e formatos de saída da captura.

use serde::{Deserialize, Serialize};

/// Porte de `reference/main.rs:354-359`.
///
/// `text` e `highlight` recebem o mesmo valor em `recortar`. Apenas
/// `highlight` é persistido (`main.rs:619`); `text` é dado morto — mantido
/// aqui para fidelidade da captura. Ver ESPECIFICACAO.md §5.4.
#[derive(Debug, Clone, Default)]
pub struct Recorte {
    pub page: u64,
    pub text: String,
    pub highlight: String,
}

/// Porte de `reference/main.rs:538-542`.
///
/// A ORDEM das chaves no arquivo de entrada é normativa: ela deve reproduzir
/// `ORDER BY tp.id_perfil, tpv.expressao_nm` (`main.rs:560-562`), porque
/// governa a deduplicação por perfil (INV-P12).
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ChavePesquisa {
    pub id_perfil: i64,
    pub expressao_nm: String,
}

/// Uma linha do que seria gravado em `tb_recorte` + `tb_recorte_texto`.
#[derive(Debug, Clone, Serialize)]
pub struct RecorteGravado {
    /// Posição na sequência de gravação. Zero-indexada.
    pub ordem: usize,
    pub id_perfil: i64,
    pub expressao_busca: String,
    pub nr_pagina: i64,
    /// SHA-256 hexadecimal do texto gravado em `tb_recorte_texto.recorte`.
    pub texto_sha256: String,
    /// Texto integral gravado. Omitido quando `--texto sha256`.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub texto: Option<String>,
}

/// Desfecho da captura de um documento — espelha a máquina de estados.
#[derive(Debug, Clone, Serialize)]
#[serde(tag = "desfecho", rename_all = "snake_case")]
pub enum Desfecho {
    /// Equivale a `status = 5` (`main.rs:325`).
    Finalizado { total_recortes: usize },
    /// Equivale a `status = -1` por falha em `criar_indice` (`main.rs:334`).
    ErroIndexacao { detalhe: String },
    /// Equivale a `status = -1` por falha em `recortar` (`main.rs:320`).
    /// A importação encerra e o trabalho parcial permanece (INV-P14).
    ErroRecorte {
        id_perfil: i64,
        expressao_nm: String,
        detalhe: String,
        recortes_ja_gravados: usize,
    },
}

/// Arquivo `<nome>.recortes.json`.
#[derive(Debug, Clone, Serialize)]
pub struct SaidaRecortes {
    pub documento: String,
    pub sha256_pdf: String,
    #[serde(flatten)]
    pub desfecho: Desfecho,
    pub recortes: Vec<RecorteGravado>,
}

/// Arquivo `<nome>.paginas.json` e `<nome>.paginas-brutas.json`.
#[derive(Debug, Clone, Serialize)]
pub struct SaidaPaginas {
    pub documento: String,
    pub sha256_pdf: String,
    pub estagio: &'static str,
    pub total_paginas: usize,
    pub paginas: Vec<String>,
}

/// Arquivo `<nome>.tokens.json`.
#[derive(Debug, Clone, Serialize)]
pub struct SaidaTokens {
    pub documento: String,
    pub sha256_pdf: String,
    pub total_paginas: usize,
    pub termos_por_pagina: Vec<Vec<String>>,
}
