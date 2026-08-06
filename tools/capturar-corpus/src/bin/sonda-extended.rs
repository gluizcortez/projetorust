//! `sonda-extended` — sonda exploratória do modo `x` (extended) do crate
//! `regex` do Rust.
//!
//! NÃO é oráculo de teste: é o instrumento que respondeu, na fase F7, às
//! perguntas que a documentação do crate deixa em aberto e das quais INV-P01
//! depende. O oráculo de regressão contínua é `oraculo-extended`.
//!
//! Para cada padrão, compara o `Hir` de `(?imx)COM_X` com o de `(?im)SEM_X`.
//! Dois padrões com o mesmo `Hir` são o mesmo autômato — é a igualdade que
//! interessa, e é imune a como o crate imprime a sintaxe de volta.
//!
//! Uso:
//!     cargo run --release --bin sonda-extended

fn hir(padrao: &str) -> String {
    match regex_syntax::ParserBuilder::new().build().parse(padrao) {
        Ok(h) => format!("{h:?}"),
        Err(e) => format!("ERRO: {}", e.to_string().replace('\n', " | ")),
    }
}

fn comparar(rotulo: &str, com_x: &str, sem_x: &str) {
    let a = hir(&format!("(?imx){com_x}"));
    let b = hir(&format!("(?im){sem_x}"));
    let veredito = if a == b { "IGUAL " } else { "DIFERE" };
    println!("{veredito}  {rotulo}");
    println!("        (?imx) {com_x:?}");
    println!("        (?im)  {sem_x:?}");
    if a != b {
        let corte = |s: &String| {
            if s.len() > 160 {
                format!("{}…", &s[..160])
            } else {
                s.clone()
            }
        };
        println!("        A = {}", corte(&a));
        println!("        B = {}", corte(&b));
    }
    println!();
}

/// Imprime o resultado bruto de um único padrão, para os casos em que a
/// pergunta é "isto compila?" e não "isto equivale a quê?".
fn cru(rotulo: &str, padrao: &str) {
    println!("        {rotulo}: {padrao:?} → {}", hir(padrao));
}

fn main() {
    println!("=== 8. escopo da flag: (?-x) desliga o modo extended? ===");
    comparar("grupo (?-x: a b )", "A(?-x: b )C", "A( b )C");
    comparar("(?-x) solto", "A(?-x) b c", "A b c");
    comparar("(?x) ligando de novo", "(?-x)a b(?x)c d", "a b(?x)cd");

    println!("=== 9. espaço DENTRO de construções nomeadas ===");
    cru("\\p{ Lu }", "(?imx)\\p{ Lu }");
    cru("\\p{Lu}", "(?imx)\\p{Lu}");
    cru("(? i)a", "(?imx)(? i)a");
    cru("(?P< n >a)", "(?imx)(?P< n >a)");
    cru("\\x{ 41 }", "(?imx)\\x{ 41 }");
    cru("\\x{41}", "(?imx)\\x{41}");
    println!();

    println!("=== 10. escapes na fronteira ===");
    cru("barra no fim", "(?imx)AB\\");
    cru("barra dupla e cerquilha", "(?imx)AB\\\\#CD");
    cru("barra dupla e cerquilha, sem x", "(?im)AB\\\\#CD");
    cru("classe aberta no fim", "(?imx)[ab");
    println!();
    comparar("\\\\ seguido de # inicia comentário", "AB\\\\#CD\nEF", "AB\\\\EF");

    println!("=== 11. fim do comentário ===");
    comparar("comentário termina em \\n", "A#x\nB", "AB");
    comparar("comentário NÃO termina em \\r", "A#x\rB", "A");
    comparar("comentário NÃO termina em \\r (controle)", "A#x\rB", "AB");
    comparar("\\n dentro do comentário some", "A#x\n\nB", "AB");

    println!("=== 12. a expressão real, e a tradução ingênua ===");
    comparar(
        "ACME & FILHOS após substituição",
        "ACME \\s*&\\s* FILHOS",
        "ACME\\s*&\\s*FILHOS",
    );
    comparar(
        "tradução ingênua NÃO equivale",
        "ACME \\s*&\\s* FILHOS",
        "ACME \\s*&\\s* FILHOS",
    );

    println!("=== 13. \\s do Rust contra o \\s do Go ===");
    cru("\\s", "\\s");
    println!("        Go: [\\t\\n\\f\\r ] — nem \\v, nem os espaços Unicode.");
    println!();

    println!("=== 14. runas que o \\s do Rust aceita (Unicode inteiro) ===");
    let re = regex::Regex::new(r"^\s$").unwrap();
    let mut brancos = vec![];
    for cp in 0u32..=0x10FFFF {
        if let Some(c) = char::from_u32(cp) {
            if re.is_match(&c.to_string()) {
                brancos.push(cp);
            }
        }
    }
    println!("        total: {}", brancos.len());
    print!("       ");
    for cp in &brancos {
        print!(" U+{cp:04X}");
    }
    println!("\n");

    println!("=== 15. runas que o modo x DESCARTA (Unicode inteiro) ===");
    // Um caractere é descartado se `(?x)A<c>B` equivale a `(?x)AB`.
    let referencia = hir("(?x)AB");
    let mut descartadas = vec![];
    for cp in 0u32..=0x10FFFF {
        if let Some(c) = char::from_u32(cp) {
            if hir(&format!("(?x)A{c}B")) == referencia {
                descartadas.push(cp);
            }
        }
    }
    println!("        total: {}", descartadas.len());
    print!("       ");
    for cp in &descartadas {
        print!(" U+{cp:04X}");
    }
    println!();
    println!(
        "        descartadas == brancos do \\s? {}",
        descartadas == brancos
    );
}
