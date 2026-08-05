//! Índice em memória e busca de frase.
//!
//! Porte VERBATIM de `criar_indice` (parte de indexação,
//! `reference/main.rs:508-535`) e de `recortar` (`reference/main.rs:361-410`).
//!
//! O esquema é reconstruído dentro de `recortar` exatamente como no legado
//! (`main.rs:365-373`) — inclusive a fragilidade descrita no achado A09 —
//! para que a captura reproduza o comportamento real, não o corrigido.

use anyhow::Result;
use tantivy::collector::TopDocs;
use tantivy::query::QueryParser;
use tantivy::schema::Value;
use tantivy::schema::{Schema, STORED, TEXT};
use tantivy::{Index, TantivyDocument};

use crate::modelo::Recorte;

/// Reproduz `reference/main.rs:510-515` e `365-370`.
///
/// A ordem de declaração dos campos é normativa: `page` recebe o identificador
/// 0 e `text` o identificador 1, e é essa coincidência que faz o esquema
/// reconstruído em `recortar` funcionar contra o índice.
fn montar_esquema() -> Schema {
    let mut sch = Schema::builder();
    sch.add_u64_field("page", STORED);
    sch.add_text_field("text", TEXT | STORED);
    sch.build()
}

/// Reproduz `reference/main.rs:508-535`.
pub fn criar_indice(paginas: &[String]) -> Result<Index> {
    let sch = montar_esquema();
    let idx = Index::create_in_ram(sch.clone());

    // main.rs:519 — orçamento de 500 MB por importação (achado A04).
    let mut idx_writer = idx.writer(500_000_000)?;

    let page_field = sch.get_field("page")?;
    let text_field = sch.get_field("text")?;

    for (i, pagina) in paginas.iter().enumerate() {
        let mut doc = TantivyDocument::default();
        // main.rs:526 — numeração de página começa em 1.
        doc.add_u64(page_field, (i + 1) as u64);
        doc.add_text(text_field, pagina);
        idx_writer.add_document(doc)?;
    }

    idx_writer.commit()?;

    Ok(idx)
}

/// Termos do índice por página, para validar o analisador léxico isoladamente.
///
/// Aplica ao texto o MESMO analisador que o Tantivy aplica ao campo `text` e
/// que o `QueryParser` aplica à consulta. Oráculo de INV-P03 e INV-P04.
pub fn tokenizar_paginas(idx: &Index, paginas: &[String]) -> Result<Vec<Vec<String>>> {
    let sch = idx.schema();
    let text_field = sch.get_field("text")?;
    let mut analisador = idx.tokenizer_for_field(text_field)?;

    let mut por_pagina = vec![];
    for pagina in paginas {
        let mut termos = vec![];
        let mut fluxo = analisador.token_stream(pagina);
        while let Some(token) = fluxo.next() {
            termos.push(token.text.clone());
        }
        por_pagina.push(termos);
    }

    Ok(por_pagina)
}

/// Porte VERBATIM de `reference/main.rs:361-410`.
///
/// Preserva deliberadamente:
///   - a reconstrução do esquema (achado A09, `main.rs:365-373`);
///   - o limite de 1.000.000 de acertos (achado A17, `main.rs:385`);
///   - a compilação da expressão regular DENTRO do laço (achado A08, `main.rs:394`);
///   - a flag `(?imx)`, cujo modo extended é a divergência INV-P01;
///   - `text` e `highlight` recebendo o mesmo valor (ESPECIFICACAO.md §5.4).
///
/// Uma expressão contendo aspas duplas produz `Err` aqui, o que no legado
/// aborta a importação inteira — ver INV-P17.
pub fn recortar(idx: &Index, key: &str) -> Result<Vec<Recorte>> {
    let mut recortes: Vec<Recorte> = vec![];

    let sch = montar_esquema();

    let page_field = sch.get_field("page")?;
    let text_field = sch.get_field("text")?;

    // main.rs:375 — busca de FRASE, montada por interpolação sem escape.
    let query = QueryParser::for_index(idx, vec![text_field]).parse_query(&format!(r#""{key}""#))?;

    let reader = idx
        .reader_builder()
        .reload_policy(tantivy::ReloadPolicy::OnCommitWithDelay)
        .try_into()?;

    let searcher = reader.searcher();

    let top_docs = searcher.search(&query, &TopDocs::with_limit(1_000_000))?;

    for (_score, doc_address) in top_docs {
        let doc = searcher.doc::<TantivyDocument>(doc_address)?;
        let texto = doc
            .get_first(text_field)
            .unwrap()
            .as_str()
            .unwrap()
            .to_string();

        // main.rs:392-398 — filtro do operador `&`, com a flag `x` (INV-P01).
        if key.contains('&') {
            let exp = format!("(?imx){}", key.replace('&', r"\s*&\s*"));
            let re = regex::Regex::new(&exp).unwrap();
            if !re.is_match(&texto) {
                continue;
            }
        }

        recortes.push(Recorte {
            page: doc.get_first(page_field).unwrap().as_u64().unwrap(),
            text: doc
                .get_first(text_field)
                .unwrap()
                .as_str()
                .unwrap()
                .to_string(),
            highlight: texto,
        });
    }

    Ok(recortes)
}
