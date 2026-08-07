//! `sonda-caminho` — mede como o roteador do Salvo compara CAMINHOS quando o
//! caminho traz espaço, percentual-codificação e outros bytes que a sonda
//! original (`sonda-http`) nunca exercitou.
//!
//! A pergunta que originou esta sonda veio de campo: `curl` disparado com um
//! espaço sobrando na URL produziu `GET /ping%20`, e o porte em Go respondeu
//! 404. `normalizarCaminho` documenta que "segmentos vazios e `.` são
//! ignorados" — e diz, por escrito, que os demais casos NÃO foram medidos.
//! Esta sonda mede.
//!
//! O ponto delicado é ONDE cada lado decodifica:
//!
//!   - Go: `r.URL.Path` já vem DECODIFICADO, então `%20` vira espaço e `%2F`
//!     vira barra ANTES de o caminho ser partido em segmentos.
//!   - Salvo: parte `uri().path()`, que no crate `http` é o caminho CRU.
//!
//! Se as duas ordens divergirem, `/ping%2F` é o caso que separa: em Go ele
//! vira `/ping/`, que normaliza para `/ping`; no Salvo continua sendo um
//! segmento só, `ping%2F`.
//!
//! Por isso a sonda NÃO usa `TestClient`: qualquer cliente que passe por
//! `Url::parse` pode normalizar a URL antes de ela chegar ao servidor, e é
//! justamente a forma crua que interessa. Ela sobe um servidor de verdade e
//! escreve a linha de requisição byte a byte num socket.
//!
//! A saída é uma tabela em Markdown e um JSON, para que o porte em Go possa
//! ser comparado contra ela mecanicamente.
//!
//! Uso:
//!     cargo run --release --manifest-path tools/sonda-http/Cargo.toml \
//!         --bin sonda-caminho

use std::io::{Read, Write};
use std::net::TcpStream;

use salvo::prelude::*;

/// Porte de `reference/main.rs:112-115`.
#[handler]
async fn ping() -> &'static str {
    "pong"
}

/// Reconstrói o roteamento de `reference/main.rs:52-60` na parte que importa
/// para o caminho. `/pdf` entra porque é a outra rota do legado e porque o
/// prefixo comum permite perguntar se `/pdf` casa por prefixo.
#[handler]
async fn pdf() -> &'static str {
    "PDF carregado com sucesso"
}

fn montar_servico() -> Service {
    let rota_ping = Router::with_path("/ping").get(ping);
    let rota_pdf = Router::with_path("/pdf").post(pdf);

    let router = Router::new()
        .hoop(RequestId::new())
        .push(rota_ping)
        .push(rota_pdf);

    Service::new(router).hoop(Logger::new())
}

/// Os caminhos medidos, na forma CRUA em que vão para a linha de requisição.
///
/// A primeira metade repete o que a sonda original já havia medido, para que
/// esta rodada confirme aquelas conclusões em vez de assumi-las. A segunda
/// metade é o território novo.
const CAMINHOS: &[(&str, &str)] = &[
    // --- já medidos pela sonda-http, aqui como controle ---
    ("/ping", "o caminho exato"),
    ("/ping/", "barra ao final"),
    ("/ping//", "duas barras ao final"),
    ("//ping", "barra dobrada no início"),
    ("/./ping", "segmento ponto"),
    ("/ping/x", "dois segmentos"),
    ("/PING", "maiúsculas"),
    ("/naoexiste", "rota inexistente"),
    ("/", "a raiz"),
    // --- território novo: espaço ---
    ("/ping%20", "espaço ao final, percentual-codificado"),
    ("/%20ping", "espaço no início, percentual-codificado"),
    ("/ping%20%20", "dois espaços ao final"),
    ("/ping+", "mais ao final (só é espaço em query)"),
    ("/ping%09", "tabulação ao final"),
    ("/ping%0A", "quebra de linha ao final"),
    // --- território novo: a barra codificada, que separa as duas ordens ---
    ("/ping%2F", "barra codificada ao final"),
    ("/%2Fping", "barra codificada no início"),
    ("/ping%2F%2F", "duas barras codificadas ao final"),
    ("/pi%6Eg", "o próprio 'n' codificado"),
    ("/%70ing", "o próprio 'p' codificado"),
    // --- território novo: ponto-ponto e vazio ---
    ("/ping/..", "ponto-ponto depois da rota"),
    ("/x/../ping", "ponto-ponto atravessando"),
    ("/ping%2E", "ponto codificado ao final"),
    ("/./././ping", "vários segmentos ponto"),
    // --- território novo: outros bytes ---
    ("/ping%00", "byte nulo ao final"),
    ("/ping?x=1", "query string não faz parte do caminho"),
    ("/ping#f", "fragmento não é enviado por cliente real, mas cru vai"),
    // --- a raiz em suas várias formas: quantos segmentos são zero? ---
    ("//", "duas barras só"),
    ("///", "três barras só"),
    ("/.", "só o segmento ponto"),
    ("/./", "ponto e barra"),
    // --- percentual inválido: o servidor recusa ou passa adiante? ---
    ("/ping%", "percentual solto"),
    ("/ping%2", "percentual truncado"),
    ("/ping%zz", "percentual com dígitos inválidos"),
    ("/%2Eping", "ponto codificado no início"),
    // --- codificação da rota inteira ---
    ("/%70%69%6E%67", "'ping' inteiro codificado"),
    ("/ping%2f", "barra codificada em minúsculas"),
    // --- o fragmento: cliente real não manda, mas a linha crua manda ---
    ("/ping%23f", "cerquilha CODIFICADA — é literal, não fragmento"),
    ("/pdf#x", "fragmento na rota autenticada"),
    ("/ping\tx", "tabulação CRUA na linha de requisição"),
];

/// Caminhos medidos também com POST, para separar "a rota não existe" (404) de
/// "a rota existe e o método não" (405).
const CAMINHOS_POST: &[(&str, &str)] = &[
    ("/", "a raiz, com POST"),
    ("//", "duas barras, com POST"),
    ("/ping", "a rota de GET, com POST"),
    ("/naoexiste", "rota inexistente, com POST"),
    ("/pdf", "a rota de POST, sem multipart"),
];

#[derive(serde::Serialize)]
struct Medicao {
    metodo: &'static str,
    caminho: &'static str,
    nota: &'static str,
    status: u16,
    corpo: String,
}

/// Escreve a linha de requisição byte a byte e lê a resposta inteira.
///
/// Sem cliente HTTP no meio de propósito: é a única forma de garantir que
/// `/ping%20` chegue ao servidor como `/ping%20` e não como `/ping ` ou
/// `/ping`.
fn requisitar(porta: u16, metodo: &str, caminho: &str) -> std::io::Result<(u16, String)> {
    let mut fluxo = TcpStream::connect(("127.0.0.1", porta))?;
    let requisicao =
        format!("{metodo} {caminho} HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n");
    fluxo.write_all(requisicao.as_bytes())?;
    fluxo.flush()?;

    let mut bruto = Vec::new();
    fluxo.read_to_end(&mut bruto)?;
    let texto = String::from_utf8_lossy(&bruto).to_string();

    let status = texto
        .split_whitespace()
        .nth(1)
        .and_then(|s| s.parse::<u16>().ok())
        .unwrap_or(0);

    // O corpo é o que vem depois da linha em branco. Basta o começo: as
    // respostas que interessam são `pong` (4 bytes) ou a página do catcher.
    let corpo = texto
        .split_once("\r\n\r\n")
        .map(|(_, c)| c.to_string())
        .unwrap_or_default();

    Ok((status, corpo))
}

/// Reduz o corpo ao que distingue as respostas, para caber na tabela.
fn resumir(corpo: &str) -> String {
    if corpo == "pong" {
        return "pong".to_string();
    }
    if corpo.contains("salvo.rs") {
        return format!("<catcher, {} bytes>", corpo.len());
    }
    if corpo.is_empty() {
        return "<vazio>".to_string();
    }
    let primeiros: String = corpo.chars().take(40).collect();
    format!("{primeiros:?}")
}

#[tokio::main]
async fn main() {
    let porta = 6011u16;
    let servico = montar_servico();

    let acceptor = TcpListener::new(("127.0.0.1", porta)).bind().await;
    tokio::spawn(async move {
        Server::new(acceptor).serve(servico).await;
    });

    // O `bind` já ocorreu acima, então a porta está escutando; a espera é só
    // para o executor entrar no `serve`.
    tokio::time::sleep(std::time::Duration::from_millis(200)).await;

    let mut medicoes = Vec::new();
    let lote = CAMINHOS
        .iter()
        .map(|(c, n)| ("GET", *c, *n))
        .chain(CAMINHOS_POST.iter().map(|(c, n)| ("POST", *c, *n)));

    for (metodo, caminho, nota) in lote {
        // O socket é síncrono; sai da thread do executor para não bloqueá-lo.
        let (status, corpo) = tokio::task::spawn_blocking(move || requisitar(porta, metodo, caminho))
            .await
            .expect("a tarefa de socket")
            .unwrap_or_else(|e| (0, format!("<erro de socket: {e}>")));

        medicoes.push(Medicao {
            metodo,
            caminho,
            nota,
            status,
            corpo,
        });
    }

    println!("## Como o Salvo compara caminhos\n");
    println!("Salvo 0.95 (ver `tools/sonda-http/Cargo.lock`), servidor real, linha de");
    println!("requisição escrita byte a byte.\n");
    println!("| requisição crua | o que é | status | corpo |");
    println!("|---|---|---|---|");
    for m in &medicoes {
        println!(
            "| `{} {}` | {} | {} | {} |",
            m.metodo,
            m.caminho,
            m.nota,
            m.status,
            resumir(&m.corpo)
        );
    }

    let json = serde_json::to_string_pretty(&medicoes).expect("serializar");
    std::fs::write("/tmp/sonda-caminho.json", json).expect("gravar o JSON");
    eprintln!("\nJSON em /tmp/sonda-caminho.json");
}
