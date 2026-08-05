//! Extração e normalização de texto.
//!
//! Porte VERBATIM de `criar_indice` (`reference/main.rs:479-536`), com uma
//! única diferença: além do texto normalizado, captura o texto BRUTO (antes da
//! junção de hífens e da remoção de diacríticos), que a fase F5 usa como
//! oráculo isolado da fase F6.
//!
//! Nada aqui pode ser "melhorado". A ordem das operações é normativa —
//! ver docs/ESPECIFICACAO.md §4 e docs/INVARIANTES.md INV-P08.

use anyhow::Result;

/// Texto de um documento, nos dois estágios que interessam à paridade.
pub struct PaginasCapturadas {
    /// Texto por página ANTES da junção de hífens e da remoção de diacríticos.
    /// Oráculo da fase F5 (INV-P09, INV-P10, INV-P11).
    pub brutas: Vec<String>,
    /// Texto por página DEPOIS de todo o pipeline. Oráculo da fase F6.
    pub normalizadas: Vec<String>,
}

/// Reproduz `reference/main.rs:479-506`.
///
/// A normalização é aplicada POR LINHA, exatamente como no legado
/// (`main.rs:498-501`), antes da concatenação da página (`main.rs:505`).
/// A equivalência com a aplicação por página está demonstrada em
/// docs/ESPECIFICACAO.md §4.3 — mas aqui reproduzimos a forma literal.
pub fn extrair_paginas(conteudo: &[u8]) -> Result<PaginasCapturadas> {
    let documento = mupdf::Document::from_bytes(conteudo, "pdf")?;

    let mut brutas = vec![];
    let mut normalizadas = vec![];

    // main.rs:486-487
    let re_hifen_final_de_linha =
        regex::Regex::new(r#"(?imx)(\w+)(-\n)"#).expect("Expressão regular para remoção de hífens");

    for page in documento.pages()? {
        // main.rs:490
        let text_page = page?.to_text_page(mupdf::text_page::TextPageOptions::empty())?;

        let mut pagina_bruta = vec![];
        let mut pagina_normalizada = vec![];

        for block in text_page.blocks() {
            for line in block.lines() {
                // main.rs:494-497 — caractere irrecuperável vira ESPAÇO (INV-P10),
                // a linha é aparada e recebe exatamente um '\n'.
                let chars = format!(
                    "{}\n",
                    line.chars()
                        .map(|c| c.char().unwrap_or(' '))
                        .collect::<String>()
                        .trim()
                );

                pagina_bruta.push(chars.clone());

                // main.rs:498-500 — junção de hífens.
                let chars = re_hifen_final_de_linha.replace_all(&chars, "$1").to_string();
                // main.rs:501 — remoção de diacríticos, SEMPRE depois da junção.
                let chars = diacritics::remove_diacritics(&chars);

                pagina_normalizada.push(chars);
            }
        }

        // main.rs:505 — join("") sem separador entre blocos (INV-P09).
        brutas.push(pagina_bruta.join(""));
        normalizadas.push(pagina_normalizada.join(""));
    }

    Ok(PaginasCapturadas {
        brutas,
        normalizadas,
    })
}
