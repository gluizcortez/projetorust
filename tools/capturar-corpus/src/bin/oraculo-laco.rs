//! `oraculo-laco` — oráculo do LAÇO INTEIRO de recorte do legado.
//!
//! É o par em Rust do teste de propriedade da fase F12. Os oráculos anteriores
//! medem peças: `oraculo-normalizacao` mede o pipeline de texto,
//! `oraculo-extended` mede o filtro do operador `&`. Este mede o que o serviço
//! REALMENTE faz com um documento: indexa as páginas, percorre as chaves na
//! ordem, ordena, deduplica por perfil e produz os recortes na ordem de
//! gravação.
//!
//! É onde moram as invariantes que nenhum oráculo de peça alcança — INV-P12
//! (deduplicação por perfil), INV-P13 (o sentinela que inicia em zero) e
//! INV-P14 (o parcial que permanece quando uma chave falha).
//!
//! # Protocolo
//!
//! Um CASO por linha, tudo em hexadecimal para atravessar bytes arbitrários sem
//! escapar nada. Os separadores são TAB entre campos e unidade de registro
//! (`\x1e`) entre itens de uma lista:
//!
//! ```text
//! entrada : <hex pág 1>\x1e<hex pág 2>… TAB <id_perfil>:<hex expressão>\x1e…
//! saída   : <desfecho> TAB <id_perfil>:<hex expressão>:<nr_pagina>:<sha256>\x1e…
//! ```
//!
//! `desfecho` é `finalizado`, `erro_indexacao`, `erro_recorte` ou `panico`.
//!
//! O `panico` é observável e importa: `recortar` faz `.unwrap()` na expressão
//! regular do filtro `&` (`main.rs:395`), e um padrão inválido MATA a tarefa
//! sem gravar status — a importação fica presa em 3, que NÃO é o mesmo que -1.
//! Ver docs/DECISOES-ABERTAS.md, D-06, e INV-P23.
//!
//! # Uso
//!
//! ```sh
//! cargo build --release --manifest-path tools/capturar-corpus/Cargo.toml \
//!     --bin oraculo-laco
//! ```

use std::io::{self, BufRead, Write};
use std::panic;

use capturar_corpus::extracao::normalizar_pagina;
use capturar_corpus::indice::criar_indice;
use capturar_corpus::laco;
use capturar_corpus::modelo::{ChavePesquisa, Desfecho};

/// Separador entre itens de uma lista. É a unidade de registro do ASCII,
/// escolhida por não aparecer em texto hexadecimal nem em identificador.
const SEP_ITEM: char = '\x1e';

fn decodificar(hexa: &str) -> Option<String> {
    let bytes = hex::decode(hexa).ok()?;
    String::from_utf8(bytes).ok()
}

/// Analisa `<id_perfil>:<hex expressão>`.
fn analisar_chave(campo: &str) -> Option<ChavePesquisa> {
    let (id, hexa) = campo.split_once(':')?;
    Some(ChavePesquisa {
        id_perfil: id.parse().ok()?,
        expressao_nm: decodificar(hexa)?,
    })
}

fn analisar_caso(linha: &str) -> Option<(Vec<String>, Vec<ChavePesquisa>)> {
    let (paginas_hex, chaves_txt) = linha.split_once('\t')?;

    let paginas = if paginas_hex.is_empty() {
        vec![]
    } else {
        paginas_hex
            .split(SEP_ITEM)
            .map(decodificar)
            .collect::<Option<Vec<_>>>()?
    };

    let chaves = if chaves_txt.is_empty() {
        vec![]
    } else {
        chaves_txt
            .split(SEP_ITEM)
            .map(analisar_chave)
            .collect::<Option<Vec<_>>>()?
    };

    Some((paginas, chaves))
}

/// Executa o laço, capturando o pânico do `.unwrap()` do filtro `&`.
///
/// `catch_unwind` é a única forma de OBSERVAR esse desfecho sem derrubar o
/// processo — e observá-lo é obrigatório, porque ele é diferente de `-1`.
fn executar(paginas: &[String], chaves: &[ChavePesquisa]) -> String {
    // As páginas chegam BRUTAS e são normalizadas aqui, como o legado faz
    // dentro de `criar_indice`. Assim o caso gerado percorre a cadeia inteira —
    // junção de hífens, diacríticos, indexação e laço —, e não só o laço.
    let paginas: Vec<String> = paginas.iter().map(|p| normalizar_pagina(p)).collect();

    let resultado = panic::catch_unwind(|| {
        let idx = match criar_indice(&paginas) {
            Ok(i) => i,
            Err(e) => return Err(format!("{e:?}")),
        };
        Ok(laco::executar(&idx, chaves, false))
    });

    let laco_resultado = match resultado {
        Err(_) => return "panico\t".to_string(),
        Ok(Err(_)) => return "erro_indexacao\t".to_string(),
        Ok(Ok(r)) => r,
    };

    let desfecho = match laco_resultado.desfecho {
        Desfecho::Finalizado { .. } => "finalizado",
        Desfecho::ErroIndexacao { .. } => "erro_indexacao",
        Desfecho::ErroRecorte { .. } => "erro_recorte",
    };

    let gravados = laco_resultado
        .recortes
        .iter()
        .map(|r| {
            format!(
                "{}:{}:{}:{}",
                r.id_perfil,
                hex::encode(r.expressao_busca.as_bytes()),
                r.nr_pagina,
                r.texto_sha256
            )
        })
        .collect::<Vec<_>>()
        .join(&SEP_ITEM.to_string());

    format!("{desfecho}\t{gravados}")
}

fn main() -> anyhow::Result<()> {
    // O pânico do filtro `&` é ESPERADO e capturado; sem silenciar o gancho, a
    // saída de erro do processo viraria megabytes de rastro de pilha.
    panic::set_hook(Box::new(|_| {}));

    let entrada = io::stdin().lock();
    let saida = io::stdout();
    let mut saida = io::BufWriter::new(saida.lock());

    for linha in entrada.lines() {
        let linha = linha?;
        match analisar_caso(&linha) {
            Some((paginas, chaves)) => {
                writeln!(saida, "{}", executar(&paginas, &chaves))?;
            }
            None => writeln!(saida, "entrada_invalida\t")?,
        }
    }

    saida.flush()?;
    Ok(())
}
