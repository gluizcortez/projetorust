//! `sonda-arquivo` — mede ONDE o legado põe o PDF submetido, e por quanto tempo.
//!
//! A pergunta veio de fora: "onde estaria o arquivo PDF quando a API estiver em
//! produção?". A resposta que a documentação dava — **D-21**, "os bytes vivem em
//! memória durante o processamento, não há gravação em disco, nem no legado nem
//! no porte" — está certa para o porte em Go e **errada para o legado**.
//!
//! `reference/main.rs:233` não lê o corpo da requisição. Lê um ARQUIVO:
//!
//! ```ignore
//! let conteudo = tokio::fs::read(&arquivo.path()).await.unwrap();
//! ```
//!
//! `arquivo` vem de `req.file("pdf")`, e `.path()` só existe porque o Salvo já
//! gravou aquela parte do multipart em algum lugar do disco. O legado, portanto,
//! TOCA O DISCO — o porte em Go não.
//!
//! Isso levanta três perguntas que só a medição responde:
//!
//!   1. ONDE o Salvo grava?
//!   2. O arquivo SOBREVIVE ao fim da requisição? Se sobrevive, o legado deixa
//!      lixo em disco a cada submissão — e aí a diferença não é cosmética.
//!   3. Ele sobrevive ao fim da TAREFA de segundo plano? No legado o
//!      `tokio::spawn` captura `conteudo` (os bytes já lidos), não o caminho,
//!      então em tese não precisaria do arquivo — mas "em tese" não é medição.
//!
//! Uso:
//!     cargo run --release --manifest-path tools/sonda-http/Cargo.toml \
//!         --bin sonda-arquivo

use std::path::PathBuf;
use std::sync::Mutex;

use salvo::prelude::*;

/// Onde o manipulador anota o que observou, para o `main` conferir DEPOIS que a
/// requisição terminou.
static OBSERVADO: Mutex<Option<Observacao>> = Mutex::new(None);

#[derive(Debug, Clone)]
struct Observacao {
    caminho: PathBuf,
    existia_no_manipulador: bool,
    bytes_lidos: usize,
    nome_declarado: Option<String>,
}

/// Porte da parte de `reference/main.rs:203-234` que interessa: pega o arquivo,
/// pergunta o caminho e lê o conteúdo de lá.
#[handler]
async fn upload_pdf(req: &mut Request, res: &mut Response) {
    let arquivo = match req.file("pdf").await {
        Some(f) => f,
        None => {
            res.status_code(StatusCode::BAD_REQUEST);
            res.render("PDF não enviado");
            return;
        }
    };

    let caminho = arquivo.path().to_path_buf();
    let existia = caminho.exists();

    // main.rs:233, verbatim na intenção: lê do CAMINHO, não do corpo.
    let conteudo = tokio::fs::read(&caminho).await.unwrap();

    *OBSERVADO.lock().unwrap() = Some(Observacao {
        caminho,
        existia_no_manipulador: existia,
        bytes_lidos: conteudo.len(),
        nome_declarado: arquivo.name().map(str::to_string),
    });

    res.status_code(StatusCode::OK);
    res.render("PDF carregado com sucesso");
}

fn montar_servico() -> Service {
    let router = Router::new().push(Router::with_path("/pdf").post(upload_pdf));
    Service::new(router)
}

const FRONTEIRA: &str = "----sondaArquivo";

/// Monta um multipart com os quatro campos de texto e a parte `pdf`, como o
/// cliente real faz.
fn corpo_multipart(conteudo_pdf: &[u8]) -> Vec<u8> {
    let mut corpo = Vec::new();
    for (nome, valor) in [
        ("data-caderno", "2026-07-08"),
        ("data-disponibilizacao", "2026-07-08"),
        ("id-usuario", "44521"),
        ("id-caderno", "1"),
    ] {
        corpo.extend_from_slice(
            format!(
                "--{FRONTEIRA}\r\nContent-Disposition: form-data; name=\"{nome}\"\r\n\r\n{valor}\r\n"
            )
            .as_bytes(),
        );
    }
    corpo.extend_from_slice(
        format!(
            "--{FRONTEIRA}\r\nContent-Disposition: form-data; name=\"pdf\"; \
             filename=\"diario.pdf\"\r\nContent-Type: application/pdf\r\n\r\n"
        )
        .as_bytes(),
    );
    corpo.extend_from_slice(conteudo_pdf);
    corpo.extend_from_slice(format!("\r\n--{FRONTEIRA}--\r\n").as_bytes());
    corpo
}

/// Conta quantos arquivos existem no diretório temporário do sistema.
///
/// A diferença antes/depois diz se a submissão deixou lixo, mesmo que o
/// caminho exato mude a cada requisição.
fn censo_do_temporario() -> usize {
    std::fs::read_dir(std::env::temp_dir())
        .map(|d| d.count())
        .unwrap_or(0)
}

/// Resultado de uma submissão de um dado tamanho.
struct Tentativa {
    bytes: usize,
    status: u16,
    corpo: String,
    caminho: Option<PathBuf>,
    existia: bool,
    sobreviveu: bool,
}

async fn submeter(porta: u16, conteudo: &[u8]) -> Tentativa {
    *OBSERVADO.lock().unwrap() = None;

    let cliente = reqwest::Client::new();
    let resposta = cliente
        .post(format!("http://127.0.0.1:{porta}/pdf"))
        .header(
            "content-type",
            format!("multipart/form-data; boundary={FRONTEIRA}"),
        )
        .body(corpo_multipart(conteudo))
        .send()
        .await
        .expect("a requisição");

    let status = resposta.status().as_u16();
    let corpo = resposta.text().await.unwrap_or_default();

    // Dá tempo de qualquer destrutor rodar depois da resposta.
    tokio::time::sleep(std::time::Duration::from_millis(200)).await;

    let obs = OBSERVADO.lock().unwrap().clone();
    let caminho = obs.as_ref().map(|o| o.caminho.clone());
    let existia = obs.as_ref().map(|o| o.existia_no_manipulador).unwrap_or(false);
    let sobreviveu = caminho.as_ref().map(|c| c.exists()).unwrap_or(false);

    Tentativa {
        bytes: conteudo.len(),
        status,
        corpo,
        caminho,
        existia,
        sobreviveu,
    }
}

#[tokio::main]
async fn main() {
    let porta = 6012u16;
    let acceptor = TcpListener::new(("127.0.0.1", porta)).bind().await;
    tokio::spawn(async move {
        Server::new(acceptor).serve(montar_servico()).await;
    });
    tokio::time::sleep(std::time::Duration::from_millis(200)).await;

    let real = std::fs::read("exemplos/dou-secao1-2026-07-08.pdf")
        .or_else(|_| std::fs::read("../../exemplos/dou-secao1-2026-07-08.pdf"))
        .expect("o PDF de exemplo do repositório");

    // ---------------------------------------------------------------
    // 1. Onde o arquivo fica, e por quanto tempo
    // ---------------------------------------------------------------
    let pequeno = submeter(porta, b"%PDF-1.4 conteudo de teste").await;

    println!("## Onde o legado põe o PDF submetido\n");
    println!("Salvo 0.95.2, servidor real, multipart real.\n");
    println!("| o que | valor |");
    println!("|---|---|");
    match &pequeno.caminho {
        Some(c) => {
            println!("| `arquivo.path()` | `{}` |", c.display());
            println!(
                "| diretório temporário do sistema | `{}` |",
                std::env::temp_dir().display()
            );
            println!(
                "| existia DENTRO do manipulador? | **{}** |",
                if pequeno.existia { "SIM" } else { "não" }
            );
            println!(
                "| SOBREVIVEU à requisição? | **{}** |",
                if pequeno.sobreviveu {
                    "SIM — fica em disco"
                } else {
                    "NÃO — apagado no `Drop` do `FilePart`"
                }
            );
        }
        None => println!("| — | o menor conteúdo nem virou arquivo |"),
    }

    // ---------------------------------------------------------------
    // 2. O limite de tamanho, por bisseção
    // ---------------------------------------------------------------
    println!("\n## O limite de tamanho\n");
    println!("| tamanho da parte `pdf` | corpo total | status | corpo |");
    println!("|---|---|---|---|");

    for tamanho in [
        32 * 1024usize,
        64 * 1024 - 400,
        64 * 1024 - 300,
        64 * 1024,
        64 * 1024 + 1,
        128 * 1024,
    ] {
        let mut conteudo = b"%PDF-1.4\n".to_vec();
        conteudo.resize(tamanho, b'x');
        let total = corpo_multipart(&conteudo).len();
        let t = submeter(porta, &conteudo).await;
        println!(
            "| {} bytes | {} bytes | {} | `{}` |",
            t.bytes, total, t.status, t.corpo
        );
    }

    let total_real = corpo_multipart(&real).len();
    let t = submeter(porta, &real).await;
    println!(
        "| **{} bytes — o DOU real** | {} bytes | **{}** | `{}` |",
        t.bytes, total_real, t.status, t.corpo
    );

    println!(
        "\n`GLOBAL_SECURE_MAX_SIZE` do salvo_core 0.95.2 é **{}** bytes \
         (`http/request.rs:33`), e o limite vale para o CORPO INTEIRO, não só \
         para a parte de arquivo.",
        64 * 1024
    );
}
