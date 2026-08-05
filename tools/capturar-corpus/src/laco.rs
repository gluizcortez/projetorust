//! Laço de recorte: ordenação, deduplicação por perfil e ordem de gravação.
//!
//! Porte VERBATIM de `reference/main.rs:276-326`. É a parte do legado com a
//! semântica mais sutil da migração — ver docs/INVARIANTES.md, INV-P12,
//! INV-P13 e INV-P14.
//!
//! PROIBIDO paralelizar: a deduplicação depende da ordem sequencial das chaves.

use std::collections::HashSet;

use tantivy::Index;

use crate::indice::recortar;
use crate::modelo::{ChavePesquisa, Desfecho, RecorteGravado};

pub struct ResultadoLaco {
    pub desfecho: Desfecho,
    pub recortes: Vec<RecorteGravado>,
}

/// Executa o laço de `main.rs:282-326` e devolve os recortes NA ORDEM EXATA em
/// que seriam gravados no banco.
///
/// `chaves` deve chegar na ordem produzida por
/// `ORDER BY tp.id_perfil, tpv.expressao_nm`.
pub fn executar(
    idx: &Index,
    chaves: &[ChavePesquisa],
    incluir_texto: bool,
) -> ResultadoLaco {
    let mut gravados: Vec<RecorteGravado> = vec![];

    // main.rs:278-281
    let mut nr_recortes = 0usize;
    let mut pages: HashSet<u64> = HashSet::new();
    let mut id_perfil: i64 = 0; // ← INV-P13: inicia em zero, deliberadamente

    for chave in chaves {
        // main.rs:283
        let mut recortes = match recortar(idx, &chave.expressao_nm) {
            Ok(r) => r,
            Err(e) => {
                // main.rs:318-322 — status -1 e ENCERRA; o parcial permanece.
                return ResultadoLaco {
                    desfecho: Desfecho::ErroRecorte {
                        id_perfil: chave.id_perfil,
                        expressao_nm: chave.expressao_nm.clone(),
                        detalhe: format!("{e:?}"),
                        recortes_ja_gravados: gravados.len(),
                    },
                    recortes: gravados,
                };
            }
        };

        // main.rs:285 — ordenação ESTÁVEL por página.
        recortes.sort_by(|a, b| a.page.cmp(&b.page));

        // main.rs:287-290 — o conjunto só reinicia quando o PERFIL muda.
        if id_perfil != chave.id_perfil {
            pages = HashSet::new();
            id_perfil = chave.id_perfil;
        }

        // main.rs:296-304
        let mut recortes_filtrados = vec![];
        for recorte in recortes {
            let pagina = recorte.page;
            if !pages.contains(&pagina) {
                recortes_filtrados.push(recorte.clone());
            }
            // Inserção INCONDICIONAL, fora do `if` — main.rs:303.
            pages.insert(pagina);
        }

        // main.rs:306
        nr_recortes += recortes_filtrados.len();

        // main.rs:308-317 — a gravação só ocorre se houver o que gravar.
        if !recortes_filtrados.is_empty() {
            for recorte in recortes_filtrados {
                // main.rs:603 — a conversão FALHA em vez de truncar (INV-P15).
                let nr_pagina = match i64::try_from(recorte.page) {
                    Ok(v) => v,
                    Err(e) => {
                        return ResultadoLaco {
                            desfecho: Desfecho::ErroRecorte {
                                id_perfil: chave.id_perfil,
                                expressao_nm: chave.expressao_nm.clone(),
                                detalhe: format!("estouro em nr_pagina: {e:?}"),
                                recortes_ja_gravados: gravados.len(),
                            },
                            recortes: gravados,
                        };
                    }
                };

                gravados.push(RecorteGravado {
                    ordem: gravados.len(),
                    id_perfil: chave.id_perfil,
                    expressao_busca: chave.expressao_nm.clone(),
                    nr_pagina,
                    // main.rs:619 — grava `highlight`, não `text`.
                    texto_sha256: crate::sha256_hex(recorte.highlight.as_bytes()),
                    texto: incluir_texto.then(|| recorte.highlight.clone()),
                });
            }
        }
    }

    // main.rs:325-326
    ResultadoLaco {
        desfecho: Desfecho::Finalizado {
            total_recortes: nr_recortes,
        },
        recortes: gravados,
    }
}
