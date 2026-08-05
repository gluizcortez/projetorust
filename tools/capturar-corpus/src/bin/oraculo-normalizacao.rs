//! Oráculo de normalização e tokenização para o teste de propriedade da F6.
//!
//! Lê uma cadeia por linha da entrada padrão, em hexadecimal, e emite o
//! resultado do pipeline do legado aplicado a ela:
//!
//!     <normalizado em hexadecimal>\t<termo1>\x1f<termo2>\x1f...
//!
//! O hexadecimal é necessário nos dois sentidos: a entrada aleatória e o texto
//! normalizado podem conter quebras de linha e bytes de controle.

use std::io::{self, BufRead, Write};

use tantivy::schema::{Schema, STORED, TEXT};

fn main() {
    // O analisador vem do índice, exatamente como no legado: o teste exercita
    // a cadeia registrada pelo Tantivy, não uma reconstrução dela.
    let mut sch = Schema::builder();
    sch.add_u64_field("page", STORED);
    sch.add_text_field("text", TEXT | STORED);
    let sch = sch.build();
    let idx = tantivy::Index::create_in_ram(sch.clone());
    let text_field = sch.get_field("text").expect("campo text");
    let mut analisador = idx
        .tokenizer_for_field(text_field)
        .expect("analisador do campo text");

    // reference/main.rs:487
    let re_hifen = regex::Regex::new(r#"(?imx)(\w+)(-\n)"#).expect("expressao de hifens");

    let entrada = io::stdin().lock();
    let saida = io::stdout();
    let mut saida = io::BufWriter::new(saida.lock());

    for linha in entrada.lines() {
        let linha = linha.expect("lendo entrada");
        let bruto = de_hex(&linha);

        // reference/main.rs:498-501, na ordem do legado.
        let normalizado = re_hifen.replace_all(&bruto, "$1").to_string();
        let normalizado = diacritics::remove_diacritics(&normalizado);

        let mut termos = vec![];
        let mut fluxo = analisador.token_stream(&normalizado);
        while let Some(t) = fluxo.next() {
            termos.push(t.text.clone());
        }

        writeln!(saida, "{}\t{}", para_hex(&normalizado), termos.join("\u{1f}"))
            .expect("escrevendo saida");
    }
}

fn de_hex(s: &str) -> String {
    let bytes: Vec<u8> = (0..s.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap_or(b'?'))
        .collect();
    String::from_utf8_lossy(&bytes).into_owned()
}

fn para_hex(s: &str) -> String {
    s.bytes().map(|b| format!("{b:02x}")).collect()
}
