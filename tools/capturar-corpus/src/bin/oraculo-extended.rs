//! `oraculo-extended` — oráculo do filtro do operador `&` do legado.
//!
//! Reproduz `reference/main.rs:392-398` EXATAMENTE, incluindo a flag `(?imx)` e
//! o `.unwrap()`, e responde: para esta expressão e este texto, o legado
//! manteria a página ou a descartaria?
//!
//! É o par em Rust do teste de propriedade `TestPropriedadeFiltroOperador` da
//! fase F7. O corpus dourado cobre as expressões reais; este oráculo cobre o
//! espaço de construções que o corpus não alcança — escapes, comentários,
//! classes de caracteres, espaços Unicode e operadores repetidos.
//!
//! Protocolo, uma linha por caso, hexadecimal para atravessar bytes arbitrários
//! sem escapar nada:
//!
//!     entrada : <hex da expressão> TAB <hex do texto>
//!     saída   : casou | naocasou | semfiltro | panico
//!
//! `semfiltro` é o caso em que a expressão não contém `&` e o legado não chega
//! a montar expressão regular alguma.
//!
//! Uso:
//!     cargo build --release --manifest-path tools/capturar-corpus/Cargo.toml \
//!         --bin oraculo-extended

use std::io::{self, BufRead, Write};

fn decodificar(hexa: &str) -> Option<String> {
    let bytes = hex::decode(hexa).ok()?;
    String::from_utf8(bytes).ok()
}

/// Porte VERBATIM de `reference/main.rs:392-398`.
///
/// O `.unwrap()` é do legado e é preservado de propósito: é ele que transforma
/// expressão inválida em pânico, e é justamente esse desfecho que o oráculo
/// precisa reportar (D-19).
fn filtro_do_legado(key: &str, texto: &str) -> &'static str {
    if !key.contains('&') {
        return "semfiltro";
    }

    let resultado = std::panic::catch_unwind(|| {
        let exp = format!("(?imx){}", key.replace('&', r"\s*&\s*"));
        let re = regex::Regex::new(&exp).unwrap();
        re.is_match(texto)
    });

    match resultado {
        Ok(true) => "casou",
        Ok(false) => "naocasou",
        Err(_) => "panico",
    }
}

fn main() -> io::Result<()> {
    // Os pânicos são esperados e fazem parte da resposta; silencia o rastro.
    std::panic::set_hook(Box::new(|_| {}));

    let entrada = io::stdin().lock();
    let mut saida = io::BufWriter::new(io::stdout().lock());

    for linha in entrada.lines() {
        let linha = linha?;
        if linha.is_empty() {
            continue;
        }

        let mut campos = linha.splitn(2, '\t');
        let expressao = campos.next().and_then(decodificar);
        let texto = campos.next().and_then(decodificar);

        let veredito = match (expressao, texto) {
            (Some(e), Some(t)) => filtro_do_legado(&e, &t),
            _ => "entradainvalida",
        };

        writeln!(saida, "{veredito}")?;
    }

    saida.flush()
}
