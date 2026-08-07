//! Biblioteca do ferramental de captura do corpus dourado.
//!
//! Os módulos abaixo são portes VERBATIM de `reference/main.rs` — extração,
//! indexação e laço de recorte. Eles vivem numa biblioteca, e não dentro do
//! binário, porque mais de um consumidor precisa deles:
//!
//!   - `capturar-corpus` gera os oráculos do corpus versionado (fase F0);
//!   - `oraculo-laco` responde caso a caso ao teste de propriedade (fase F12).
//!
//! Duplicar o porte entre os dois seria a pior opção possível: as duas cópias
//! divergiriam em silêncio, e o oráculo deixaria de ser oráculo.
//!
//! ATENÇÃO — ver `Cargo.toml` e docs/DECISOES-ABERTAS.md, D-15: enquanto o
//! `Cargo.lock` de produção não for disponibilizado, a saída deste ferramental
//! NÃO tem valor normativo.

use sha2::{Digest, Sha256};

pub mod extracao;
pub mod indice;
pub mod laco;
pub mod modelo;

/// Porte de `reference/main.rs:342-346`.
pub fn sha256_hex(bytes: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(bytes);
    hex::encode(hasher.finalize())
}
