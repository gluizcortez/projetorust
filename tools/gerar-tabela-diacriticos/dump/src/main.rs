//! Despeja o comportamento do Rust para cada runa do plano básico multilíngue.
//!
//! Formato: TSV com uma linha por runa.
//!
//!     <codepoint hex>\t<alfanumerico:0|1>\t<caractere-de-palavra:0|1>\t<saida de remove_diacritics>
//!
//! A saída de `remove_diacritics` pode ter zero, um ou vários caracteres, e é
//! escapada como sequência de codepoints separados por vírgula para que a
//! leitura não dependa de codificação.

fn main() {
    // `\w` do crate `regex` é [\p{Alphabetic}\p{M}\p{Nd}\p{Pc}\p{Join_Control}].
    // Em vez de reproduzir essa definição por escrito, perguntamos ao próprio
    // motor — é ele que o legado usa em reference/main.rs:487.
    let re_palavra = regex::Regex::new(r"^\w$").expect("classe de palavra");

    let mut saida = String::with_capacity(1 << 20);
    for cp in 0u32..=0xFFFF {
        // Substitutos não são runas válidas.
        if (0xD800..=0xDFFF).contains(&cp) {
            continue;
        }
        let Some(c) = char::from_u32(cp) else { continue };

        let alnum = u8::from(c.is_alphanumeric());
        let palavra = u8::from(re_palavra.is_match(&c.to_string()));

        let entrada = c.to_string();
        let mapeado = diacritics::remove_diacritics(&entrada);
        let codepoints: Vec<String> =
            mapeado.chars().map(|m| format!("{:X}", m as u32)).collect();

        saida.push_str(&format!("{cp:X}\t{alnum}\t{palavra}\t{}\n", codepoints.join(",")));
    }
    print!("{saida}");
}
