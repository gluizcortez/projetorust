//! `capturar-corpus` — captura o comportamento do serviço legado como corpus
//! dourado de paridade. Entregável 4 da fase F0.
//!
//! Para cada PDF de um diretório, grava cinco arquivos:
//!
//! | Arquivo | Conteúdo | Oráculo da fase |
//! |---|---|---|
//! | `<nome>.paginas-brutas.json` | texto por página ANTES da normalização | F5 |
//! | `<nome>.paginas.json` | texto por página DEPOIS da normalização | F6 |
//! | `<nome>.tokens.json` | termos do índice por página | F6 |
//! | `<nome>.busca.json` | páginas devolvidas por expressão, ANTES da deduplicação | F7 |
//! | `<nome>.recortes.json` | recortes na ordem exata de gravação | F8 |
//!
//! O binário é DETERMINÍSTICO: duas execuções sobre a mesma entrada produzem
//! saídas idênticas byte a byte.
//!
//! Uso:
//!     capturar-corpus <dir-corpus> <dir-saida> [--chaves ARQ] [--expressoes ARQ] [--texto MODO]
//!
//!     --chaves ARQ    JSON com [{"id_perfil":N,"expressao_nm":"..."}], NA ORDEM
//!                     produzida por `ORDER BY tp.id_perfil, tpv.expressao_nm`.
//!                     Padrão: <dir-corpus>/chaves.json
//!     --expressoes ARQ JSON com ["expressão", ...] a capturar no `.busca.json`
//!                     ALÉM das de --chaves. Opcional; ausente é aceito.
//!                     Padrão: <dir-corpus>/expressoes-busca.json
//!     --texto MODO    `completo` (padrão) grava o texto integral de cada
//!                     recorte; `sha256` grava apenas o resumo.

mod extracao;
mod indice;
mod laco;
mod modelo;

use std::path::{Path, PathBuf};

use anyhow::{bail, Context, Result};
use sha2::{Digest, Sha256};

use modelo::{ChavePesquisa, Desfecho, SaidaBuscas, SaidaPaginas, SaidaRecortes, SaidaTokens};

/// Porte de `reference/main.rs:342-346`.
pub fn sha256_hex(bytes: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    hex::encode(hasher.finalize())
}

struct Opcoes {
    dir_corpus: PathBuf,
    dir_saida: PathBuf,
    arquivo_chaves: PathBuf,
    /// Expressões EXTRA a capturar no `.busca.json`, além das de `chaves.json`.
    /// Existem para exercitar a busca em casos que o laço de recorte não
    /// alcança — INV-P17, por exemplo, aborta a importação inteira e não pode
    /// entrar no `chaves.json` sem destruir o oráculo de F8.
    arquivo_expressoes: PathBuf,
    incluir_texto: bool,
}

fn analisar_argumentos() -> Result<Opcoes> {
    let args: Vec<String> = std::env::args().skip(1).collect();

    let mut posicionais = vec![];
    let mut arquivo_chaves: Option<PathBuf> = None;
    let mut arquivo_expressoes: Option<PathBuf> = None;
    let mut incluir_texto = true;

    let mut i = 0;
    while i < args.len() {
        match args[i].as_str() {
            "--chaves" => {
                i += 1;
                arquivo_chaves = Some(PathBuf::from(
                    args.get(i).context("--chaves exige um caminho")?,
                ));
            }
            "--expressoes" => {
                i += 1;
                arquivo_expressoes = Some(PathBuf::from(
                    args.get(i).context("--expressoes exige um caminho")?,
                ));
            }
            "--texto" => {
                i += 1;
                match args.get(i).map(String::as_str) {
                    Some("completo") => incluir_texto = true,
                    Some("sha256") => incluir_texto = false,
                    outro => bail!("--texto aceita `completo` ou `sha256`, recebeu {outro:?}"),
                }
            }
            "-h" | "--help" => {
                eprintln!("{}", AJUDA);
                std::process::exit(0);
            }
            outro if outro.starts_with('-') => bail!("opção desconhecida: {outro}"),
            outro => posicionais.push(PathBuf::from(outro)),
        }
        i += 1;
    }

    if posicionais.len() != 2 {
        bail!("esperado <dir-corpus> <dir-saida>\n\n{AJUDA}");
    }

    let dir_corpus = posicionais[0].clone();
    let arquivo_chaves = arquivo_chaves.unwrap_or_else(|| dir_corpus.join("chaves.json"));
    let arquivo_expressoes =
        arquivo_expressoes.unwrap_or_else(|| dir_corpus.join("expressoes-busca.json"));

    Ok(Opcoes {
        dir_corpus,
        dir_saida: posicionais[1].clone(),
        arquivo_chaves,
        arquivo_expressoes,
        incluir_texto,
    })
}

const AJUDA: &str = "\
capturar-corpus <dir-corpus> <dir-saida> [--chaves ARQ] [--expressoes ARQ] \
[--texto completo|sha256]";

fn main() -> Result<()> {
    let op = analisar_argumentos()?;

    std::fs::create_dir_all(&op.dir_saida)
        .with_context(|| format!("criando {}", op.dir_saida.display()))?;

    let chaves = carregar_chaves(&op.arquivo_chaves)?;
    eprintln!(
        "chaves de pesquisa: {} (de {})",
        chaves.len(),
        op.arquivo_chaves.display()
    );

    let extras = carregar_expressoes_extra(&op.arquivo_expressoes)?;
    let expressoes = expressoes_a_capturar(&chaves, &extras);
    eprintln!(
        "expressões a buscar: {} ({} extra de {})",
        expressoes.len(),
        extras.len(),
        op.arquivo_expressoes.display()
    );

    let pdfs = listar_pdfs(&op.dir_corpus)?;
    if pdfs.is_empty() {
        bail!("nenhum PDF encontrado em {}", op.dir_corpus.display());
    }
    eprintln!("documentos: {}", pdfs.len());

    let mut falhas = 0usize;
    for pdf in &pdfs {
        match capturar(pdf, &chaves, &expressoes, &op) {
            Ok(desfecho) => eprintln!("  ok   {}  {}", nome_base(pdf), resumo(&desfecho)),
            Err(e) => {
                falhas += 1;
                eprintln!("  ERRO {}  {e:#}", nome_base(pdf));
            }
        }
    }

    eprintln!(
        "\nconcluído: {} documento(s), {} falha(s) de captura",
        pdfs.len(),
        falhas
    );

    if falhas > 0 {
        std::process::exit(1);
    }
    Ok(())
}

fn resumo(d: &Desfecho) -> String {
    match d {
        Desfecho::Finalizado { total_recortes } => format!("finalizado, {total_recortes} recorte(s)"),
        Desfecho::ErroIndexacao { .. } => "erro de indexação (status -1)".into(),
        Desfecho::ErroRecorte {
            recortes_ja_gravados,
            ..
        } => format!("erro de recorte (status -1), {recortes_ja_gravados} parcial(is)"),
    }
}

fn nome_base(p: &Path) -> String {
    p.file_stem().unwrap_or_default().to_string_lossy().into()
}

/// Lista os PDFs em ordem determinística.
fn listar_pdfs(dir: &Path) -> Result<Vec<PathBuf>> {
    let mut pdfs: Vec<PathBuf> = std::fs::read_dir(dir)
        .with_context(|| format!("lendo {}", dir.display()))?
        .filter_map(|e| e.ok())
        .map(|e| e.path())
        .filter(|p| {
            p.extension()
                .map(|x| x.eq_ignore_ascii_case("pdf"))
                .unwrap_or(false)
        })
        .collect();
    pdfs.sort();
    Ok(pdfs)
}

fn carregar_chaves(arq: &Path) -> Result<Vec<ChavePesquisa>> {
    let bruto = std::fs::read_to_string(arq)
        .with_context(|| format!("lendo chaves de {}", arq.display()))?;
    let chaves: Vec<ChavePesquisa> =
        serde_json::from_str(&bruto).with_context(|| format!("analisando {}", arq.display()))?;
    Ok(chaves)
}

/// Lê as expressões extra. Arquivo ausente é aceito e devolve lista vazia — o
/// oráculo continua útil só com as expressões de `chaves.json`.
fn carregar_expressoes_extra(arq: &Path) -> Result<Vec<String>> {
    if !arq.exists() {
        return Ok(vec![]);
    }
    let bruto = std::fs::read_to_string(arq)
        .with_context(|| format!("lendo expressões de {}", arq.display()))?;
    let extras: Vec<String> =
        serde_json::from_str(&bruto).with_context(|| format!("analisando {}", arq.display()))?;
    Ok(extras)
}

/// Monta a lista de expressões do `.busca.json`: as de `chaves.json` primeiro,
/// na ordem em que aparecem, seguidas das extras. Sem repetição — o mesmo texto
/// pode pertencer a mais de um perfil (ver "BETA CONSTRUCOES" no corpus), e a
/// busca não depende do perfil.
fn expressoes_a_capturar(chaves: &[ChavePesquisa], extras: &[String]) -> Vec<String> {
    let mut vistas = std::collections::HashSet::new();
    let mut saida = vec![];
    for texto in chaves
        .iter()
        .map(|c| c.expressao_nm.clone())
        .chain(extras.iter().cloned())
    {
        if vistas.insert(texto.clone()) {
            saida.push(texto);
        }
    }
    saida
}

fn capturar(
    pdf: &Path,
    chaves: &[ChavePesquisa],
    expressoes: &[String],
    op: &Opcoes,
) -> Result<Desfecho> {
    let conteudo = std::fs::read(pdf).with_context(|| format!("lendo {}", pdf.display()))?;
    let sha_pdf = sha256_hex(&conteudo);
    let documento = nome_base(pdf);

    // Estágio 1 e 2 — extração e normalização (main.rs:479-506).
    let paginas = match extracao::extrair_paginas(&conteudo) {
        Ok(p) => p,
        Err(e) => {
            // main.rs:332-335 — falha em criar_indice leva a status -1.
            let desfecho = Desfecho::ErroIndexacao {
                detalhe: format!("{e:#}"),
            };
            gravar(
                &op.dir_saida,
                &format!("{documento}.recortes.json"),
                &SaidaRecortes {
                    documento: documento.clone(),
                    sha256_pdf: sha_pdf,
                    desfecho: desfecho.clone(),
                    recortes: vec![],
                },
            )?;
            return Ok(desfecho);
        }
    };

    gravar(
        &op.dir_saida,
        &format!("{documento}.paginas-brutas.json"),
        &SaidaPaginas {
            documento: documento.clone(),
            sha256_pdf: sha_pdf.clone(),
            estagio: "bruto",
            total_paginas: paginas.brutas.len(),
            paginas: paginas.brutas.clone(),
        },
    )?;

    gravar(
        &op.dir_saida,
        &format!("{documento}.paginas.json"),
        &SaidaPaginas {
            documento: documento.clone(),
            sha256_pdf: sha_pdf.clone(),
            estagio: "normalizado",
            total_paginas: paginas.normalizadas.len(),
            paginas: paginas.normalizadas.clone(),
        },
    )?;

    // Estágio 3 — índice em memória (main.rs:508-535).
    let idx = match indice::criar_indice(&paginas.normalizadas) {
        Ok(i) => i,
        Err(e) => {
            let desfecho = Desfecho::ErroIndexacao {
                detalhe: format!("{e:#}"),
            };
            gravar(
                &op.dir_saida,
                &format!("{documento}.recortes.json"),
                &SaidaRecortes {
                    documento: documento.clone(),
                    sha256_pdf: sha_pdf,
                    desfecho: desfecho.clone(),
                    recortes: vec![],
                },
            )?;
            return Ok(desfecho);
        }
    };

    let termos = indice::tokenizar_paginas(&idx, &paginas.normalizadas)?;
    gravar(
        &op.dir_saida,
        &format!("{documento}.tokens.json"),
        &SaidaTokens {
            documento: documento.clone(),
            sha256_pdf: sha_pdf.clone(),
            total_paginas: termos.len(),
            termos_por_pagina: termos,
        },
    )?;

    // Estágio 4 — busca por expressão, ANTES da deduplicação. Oráculo de F7.
    gravar(
        &op.dir_saida,
        &format!("{documento}.busca.json"),
        &SaidaBuscas {
            documento: documento.clone(),
            sha256_pdf: sha_pdf.clone(),
            total_paginas: paginas.normalizadas.len(),
            paginas_sha256: paginas
                .normalizadas
                .iter()
                .map(|p| sha256_hex(p.as_bytes()))
                .collect(),
            buscas: indice::capturar_buscas(&idx, expressoes)?,
        },
    )?;

    // Estágio 5 — laço de recorte (main.rs:282-326).
    let resultado = laco::executar(&idx, chaves, op.incluir_texto);

    gravar(
        &op.dir_saida,
        &format!("{documento}.recortes.json"),
        &SaidaRecortes {
            documento,
            sha256_pdf: sha_pdf,
            desfecho: resultado.desfecho.clone(),
            recortes: resultado.recortes,
        },
    )?;

    Ok(resultado.desfecho)
}

/// Gravação determinística: JSON identado com quebra de linha final.
fn gravar<T: serde::Serialize>(dir: &Path, nome: &str, valor: &T) -> Result<()> {
    let caminho = dir.join(nome);
    let mut json = serde_json::to_string_pretty(valor)
        .with_context(|| format!("serializando {nome}"))?;
    json.push('\n');
    std::fs::write(&caminho, json).with_context(|| format!("gravando {}", caminho.display()))?;
    Ok(())
}
