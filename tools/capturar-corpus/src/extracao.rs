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

/// Aplica a normalização do legado a um texto de página JÁ PAGINADO.
///
/// Existe para o teste de propriedade da fase F12, que gera texto sintético em
/// vez de extraí-lo de um PDF: `extrair_paginas` está amarrada ao MuPDF e não
/// serve para uma cadeia qualquer.
///
/// A reprodução é LINHA A LINHA, exatamente como `main.rs:494-501`: cada linha
/// é aparada, recebe um `\n`, passa pela junção de hífens e pela remoção de
/// diacríticos, e só então as linhas são concatenadas SEM separador
/// (`main.rs:505`).
///
/// A junção de hífens REMOVE o `\n` junto com o hífen — é assim que
/// `conti-\nnuacao` vira `continuacao`: o `$1` engole os dois, e a linha
/// seguinte encosta na anterior.
pub fn normalizar_pagina(bruta: &str) -> String {
    let re_hifen_final_de_linha =
        regex::Regex::new(r#"(?imx)(\w+)(-\n)"#).expect("Expressão regular para remoção de hífens");

    let mut linhas: Vec<&str> = bruta.split('\n').collect();
    // O `\n` FINAL da página fecha a última linha; ele não abre uma linha nova.
    // Sem descartar o pedaço vazio que o `split` deixa, a normalização
    // acrescentaria um `\n` que o legado não produz — e a página inteira
    // divergiria por um byte no fim.
    if linhas.last() == Some(&"") {
        linhas.pop();
    }

    linhas
        .into_iter()
        .map(|linha| {
            let chars = format!("{}\n", linha.trim());
            let chars = re_hifen_final_de_linha.replace_all(&chars, "$1").to_string();
            diacritics::remove_diacritics(&chars)
        })
        .collect::<Vec<_>>()
        .join("")
}
