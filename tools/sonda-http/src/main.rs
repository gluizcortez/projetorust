//! `sonda-http` — mede o contrato HTTP do legado nos pontos em que a
//! especificação ficou marcada como `INFERIDO — confirmar`.
//!
//! Reconstrói o roteador de `reference/main.rs:52-60` com o mesmo encadeamento
//! de hoops e pergunta ao próprio Salvo:
//!
//!   D-08  o que responde uma rota inexistente e um método não permitido
//!   D-10  qual o `Content-Type` exato de cada uma das respostas
//!
//! Também responde a três perguntas menores que a especificação registrou como
//! inferidas: se o cabeçalho `X-API-KEY` é insensível a maiúsculas, o que
//! acontece com cabeçalho repetido, e qual valor de campo multipart prevalece
//! quando o nome se repete.
//!
//! A saída é uma tabela em Markdown, pronta para colar em ESPECIFICACAO.md.
//!
//! Uso:
//!     cargo run --release --manifest-path tools/sonda-http/Cargo.toml

use salvo::http::header::HeaderValue;
use salvo::prelude::*;
use salvo::test::{ResponseExt, TestClient};

/// Mesma constante de `reference/main.rs:26`.
const API_KEY: &str = "01956cb2-2f85-7440-9767-1a6651c10e0f";

/// Porte de `reference/main.rs:112-115`.
#[handler]
async fn ping() -> &'static str {
    "pong"
}

/// Porte VERBATIM de `reference/main.rs:117-131`.
///
/// Preserva a ausência de `ctrl.skip_rest()`: no Salvo, o curto-circuito da
/// cadeia é IMPLÍCITO e vem de `res.status_code(...)` com código de erro. Se
/// isso não fosse verdade, o manipulador rodaria depois do 401 — e é uma das
/// coisas que a sonda verifica.
#[handler]
async fn autenticar(req: &Request, res: &mut Response) {
    if let Some(key) = req.headers().get("X-API-KEY") {
        if key != API_KEY {
            res.status_code(StatusCode::UNAUTHORIZED);
            res.render(Text::Plain("X-API-KEY inválida"));
            return;
        }
    } else {
        res.status_code(StatusCode::UNAUTHORIZED);
        res.render(Text::Plain("Faltou a X-API-KEY"));
        return;
    }
}

/// Porte reduzido de `reference/main.rs:132-253`.
///
/// Vai até o fim da validação e para antes do banco. As três respostas que
/// interessam à sonda — 400 com as críticas, 422 e 200 — são renderizadas
/// exatamente como no legado, que é o que determina o `Content-Type`:
///
///   `res.render(&mensagem)`               — 400, um `&String`
///   `res.render("Erro ao processar o PDF")` — 422, um `&'static str`
///   `res.render("PDF carregado com sucesso")` — 200, idem
///
/// O campo `forcar-422` existe só para alcançar R4 sem banco.
#[handler]
async fn upload_pdf(req: &mut Request, res: &mut Response) {
    let _id_requisicao = req.header::<String>("x-request-id").unwrap_or_default();

    let data_caderno = req.form::<String>("data-caderno").await;
    let data_disponibilizacao = req.form::<String>("data-disponibilizacao").await;
    let id_usuario = req.form::<String>("id-usuario").await;
    let id_caderno = req.form::<String>("id-caderno").await;
    let arquivo = req.file("pdf").await;

    let mut criticas = vec![];

    // main.rs:151-162
    if let Some(v) = data_caderno {
        if chrono::NaiveDate::parse_from_str(&v, "%Y-%m-%d").is_err() {
            criticas.push("Data do caderno é inválida");
        }
    } else {
        criticas.push("Data do caderno não informada");
    }

    // main.rs:164-175
    if let Some(v) = data_disponibilizacao {
        if chrono::NaiveDate::parse_from_str(&v, "%Y-%m-%d").is_err() {
            criticas.push("Data de disponibilização é inválida");
        }
    } else {
        criticas.push("Data de disponibilização não informada");
    }

    // main.rs:177-188
    if let Some(v) = id_usuario {
        if v.parse::<i64>().is_err() {
            criticas.push("Id do usuário é inválido");
        }
    } else {
        criticas.push("Id do usuário não informado");
    }

    // main.rs:190-201
    if let Some(v) = id_caderno {
        if v.parse::<i32>().is_err() {
            criticas.push("Id do caderno é inválido");
        }
    } else {
        criticas.push("Id do caderno não informado");
    }

    // main.rs:203-214
    if let Some(v) = arquivo {
        if v.name().is_none() {
            criticas.push("PDF não possui nome");
        }
    } else {
        criticas.push("PDF não enviado");
    }

    // main.rs:218-224
    if !criticas.is_empty() {
        let mensagem = criticas.join(",");
        res.status_code(StatusCode::BAD_REQUEST);
        res.render(&mensagem);
        return;
    }

    // Substitui `registrar_pdf` falhando (main.rs:239-244).
    if req.form::<String>("forcar-422").await.is_some() {
        res.status_code(StatusCode::UNPROCESSABLE_ENTITY);
        res.render("Erro ao processar o PDF");
        return;
    }

    // main.rs:339-340
    res.status_code(StatusCode::OK);
    res.render("PDF carregado com sucesso");
}

/// Reconstrói `reference/main.rs:52-60`.
fn montar_servico() -> Service {
    let rota_ping = Router::with_path("/ping").get(ping);
    let rota_pdf = Router::with_path("/pdf")
        .hoop(autenticar)
        .hoop(salvo::affix_state::inject(std::sync::Arc::new(0usize)))
        .post(upload_pdf);

    let router = Router::new()
        .hoop(RequestId::new())
        .push(rota_ping)
        .push(rota_pdf);

    Service::new(router).hoop(Logger::new())
}

const FRONTEIRA: &str = "----sondaF9";
const MULTIPART_TIPO: &str = "multipart/form-data; boundary=----sondaF9";

fn campo(nome: &str, valor: &str) -> String {
    format!("--{FRONTEIRA}\r\nContent-Disposition: form-data; name=\"{nome}\"\r\n\r\n{valor}\r\n")
}

/// Monta um corpo multipart com os quatro campos de texto e a parte `pdf`.
///
/// `nome_do_arquivo` a `None` omite o parâmetro `filename`, que é como se
/// alcança a crítica "PDF não possui nome" (main.rs:203-209).
fn multipart_completo(nome_do_arquivo: Option<&str>, forcar_422: Option<&str>) -> String {
    let mut corpo = String::new();
    corpo.push_str(&campo("data-caderno", "2024-03-15"));
    corpo.push_str(&campo("data-disponibilizacao", "2024-03-16"));
    corpo.push_str(&campo("id-usuario", "7"));
    corpo.push_str(&campo("id-caderno", "9"));
    if let Some(v) = forcar_422 {
        corpo.push_str(&campo("forcar-422", v));
    }

    let disposicao = match nome_do_arquivo {
        Some(nome) => format!("form-data; name=\"pdf\"; filename=\"{nome}\""),
        None => "form-data; name=\"pdf\"".to_string(),
    };
    corpo.push_str(&format!(
        "--{FRONTEIRA}\r\nContent-Disposition: {disposicao}\r\nContent-Type: application/pdf\r\n\r\n%PDF-1.4 conteudo\r\n"
    ));
    corpo.push_str(&format!("--{FRONTEIRA}--\r\n"));
    corpo
}

/// Repete `data-caderno` com dois valores, para medir qual prevalece.
fn multipart_repetido(primeiro: &str, segundo: &str) -> String {
    let mut corpo = String::new();
    corpo.push_str(&campo("data-caderno", primeiro));
    corpo.push_str(&campo("data-caderno", segundo));
    corpo.push_str(&campo("data-disponibilizacao", "2024-03-16"));
    corpo.push_str(&campo("id-usuario", "7"));
    corpo.push_str(&campo("id-caderno", "9"));
    corpo.push_str(&format!(
        "--{FRONTEIRA}\r\nContent-Disposition: form-data; name=\"pdf\"; filename=\"d.pdf\"\r\n\r\n%PDF\r\n"
    ));
    corpo.push_str(&format!("--{FRONTEIRA}--\r\n"));
    corpo
}

struct Medicao {
    caso: &'static str,
    status: u16,
    content_type: String,
    corpo: String,
}

fn cabecalho(res: &Response, nome: &str) -> String {
    res.headers()
        .get(nome)
        .map(HeaderValue::to_str)
        .transpose()
        .ok()
        .flatten()
        .unwrap_or("<ausente>")
        .to_string()
}

async fn medir(caso: &'static str, mut res: Response) -> Medicao {
    let status = res.status_code.map(|c| c.as_u16()).unwrap_or(0);
    let content_type = cabecalho(&res, "content-type");
    let corpo = res.take_string().await.unwrap_or_else(|e| format!("<erro: {e}>"));
    Medicao {
        caso,
        status,
        content_type,
        corpo,
    }
}

#[tokio::main]
async fn main() {
    let servico = montar_servico();
    let mut medicoes = vec![];

    // ---------------------------------------------------------------
    // D-10 — Content-Type de cada resposta do contrato
    // ---------------------------------------------------------------

    medicoes.push(
        medir(
            "GET /ping",
            TestClient::get("http://localhost/ping").send(&servico).await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /pdf sem X-API-KEY (R1)",
            TestClient::post("http://localhost/pdf").send(&servico).await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /pdf com chave errada (R2)",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", "errada", true)
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /pdf autenticado, sem campos (R3)",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", API_KEY, true)
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /pdf forçando falha de registro (R4)",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", API_KEY, true)
                .form(&[
                    ("data-caderno", "2024-03-15"),
                    ("data-disponibilizacao", "2024-03-16"),
                    ("id-usuario", "7"),
                    ("id-caderno", "9"),
                    ("forcar-422", "1"),
                ])
                .send(&servico)
                .await,
        )
        .await,
    );

    // ---------------------------------------------------------------
    // D-08 — rota inexistente e método não permitido
    // ---------------------------------------------------------------

    medicoes.push(
        medir(
            "GET /naoexiste (rota inexistente)",
            TestClient::get("http://localhost/naoexiste")
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "GET / (raiz)",
            TestClient::get("http://localhost/").send(&servico).await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /ping (método não permitido)",
            TestClient::post("http://localhost/ping")
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "GET /pdf (método não permitido em rota com hoop)",
            TestClient::get("http://localhost/pdf")
                .add_header("X-API-KEY", API_KEY, true)
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "GET /pdf SEM chave (o hoop roda antes do 405?)",
            TestClient::get("http://localhost/pdf").send(&servico).await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "DELETE /ping",
            TestClient::delete("http://localhost/ping")
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "GET /ping/ (barra ao final)",
            TestClient::get("http://localhost/ping/")
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "GET /PING (maiúsculas)",
            TestClient::get("http://localhost/PING").send(&servico).await,
        )
        .await,
    );

    // ---------------------------------------------------------------
    // Inferências menores da §1.4.4
    // ---------------------------------------------------------------

    medicoes.push(
        medir(
            "POST /pdf com x-api-key minúsculo",
            TestClient::post("http://localhost/pdf")
                .add_header("x-api-key", API_KEY, true)
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /pdf com X-API-KEY vazia",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", "", true)
                .send(&servico)
                .await,
        )
        .await,
    );

    // Cabeçalho repetido: o primeiro é o correto, o segundo é lixo.
    medicoes.push(
        medir(
            "POST /pdf com X-API-KEY repetida (1ª correta)",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", API_KEY, false)
                .add_header("X-API-KEY", "errada", false)
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /pdf com X-API-KEY repetida (1ª errada)",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", "errada", false)
                .add_header("X-API-KEY", API_KEY, false)
                .send(&servico)
                .await,
        )
        .await,
    );

    // Campo multipart repetido: qual valor prevalece.
    medicoes.push(
        medir(
            "POST /pdf com data-caderno repetida (1ª válida, 2ª inválida)",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", API_KEY, true)
                .form(&[
                    ("data-caderno", "2024-03-15"),
                    ("data-caderno", "nao-e-data"),
                    ("data-disponibilizacao", "2024-03-16"),
                    ("id-usuario", "7"),
                    ("id-caderno", "9"),
                ])
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /pdf com data-caderno repetida (1ª inválida, 2ª válida)",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", API_KEY, true)
                .form(&[
                    ("data-caderno", "nao-e-data"),
                    ("data-caderno", "2024-03-15"),
                    ("data-disponibilizacao", "2024-03-16"),
                    ("id-usuario", "7"),
                    ("id-caderno", "9"),
                ])
                .send(&servico)
                .await,
        )
        .await,
    );

    // ---------------------------------------------------------------
    // D-08 — o catcher do Salvo negocia conteúdo?
    // ---------------------------------------------------------------

    for aceita in ["application/json", "text/plain", "text/html", "*/*", "application/xml"] {
        let caso: &'static str = Box::leak(format!("GET /naoexiste com Accept: {aceita}").into_boxed_str());
        medicoes.push(
            medir(
                caso,
                TestClient::get("http://localhost/naoexiste")
                    .add_header("accept", aceita, true)
                    .send(&servico)
                    .await,
            )
            .await,
        );
    }

    // ---------------------------------------------------------------
    // R4 e R5 — exigem multipart de verdade, com arquivo
    // ---------------------------------------------------------------

    medicoes.push(
        medir(
            "POST /pdf multipart completo (R5)",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", API_KEY, true)
                .add_header("content-type", MULTIPART_TIPO, true)
                .body(multipart_completo(Some("diario.pdf"), None))
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /pdf multipart forçando 422 (R4)",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", API_KEY, true)
                .add_header("content-type", MULTIPART_TIPO, true)
                .body(multipart_completo(Some("diario.pdf"), Some("1")))
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /pdf multipart com arquivo SEM nome",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", API_KEY, true)
                .add_header("content-type", MULTIPART_TIPO, true)
                .body(multipart_completo(None, None))
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /pdf multipart com data-caderno repetida (1ª válida)",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", API_KEY, true)
                .add_header("content-type", MULTIPART_TIPO, true)
                .body(multipart_repetido("2024-03-15", "nao-e-data"))
                .send(&servico)
                .await,
        )
        .await,
    );

    medicoes.push(
        medir(
            "POST /pdf multipart com data-caderno repetida (1ª inválida)",
            TestClient::post("http://localhost/pdf")
                .add_header("X-API-KEY", API_KEY, true)
                .add_header("content-type", MULTIPART_TIPO, true)
                .body(multipart_repetido("nao-e-data", "2024-03-15"))
                .send(&servico)
                .await,
        )
        .await,
    );

    // A crítica "PDF não possui nome" (main.rs:207) exige que `req.file("pdf")`
    // devolva Some com `name()` a None. Estas três variações procuram esse
    // estado; se nenhuma o alcançar, a crítica é INALCANÇÁVEL, como o ramo de
    // main.rs:226-230.
    for (rotulo, disposicao) in [
        ("filename vazio", "form-data; name=\"pdf\"; filename=\"\""),
        ("sem filename, com Content-Type", "form-data; name=\"pdf\""),
        ("filename só com espaço", "form-data; name=\"pdf\"; filename=\" \""),
    ] {
        let caso: &'static str =
            Box::leak(format!("POST /pdf parte pdf com {rotulo}").into_boxed_str());
        let mut corpo = String::new();
        corpo.push_str(&campo("data-caderno", "2024-03-15"));
        corpo.push_str(&campo("data-disponibilizacao", "2024-03-16"));
        corpo.push_str(&campo("id-usuario", "7"));
        corpo.push_str(&campo("id-caderno", "9"));
        corpo.push_str(&format!(
            "--{FRONTEIRA}\r\nContent-Disposition: {disposicao}\r\nContent-Type: application/pdf\r\n\r\n%PDF\r\n"
        ));
        corpo.push_str(&format!("--{FRONTEIRA}--\r\n"));

        medicoes.push(
            medir(
                caso,
                TestClient::post("http://localhost/pdf")
                    .add_header("X-API-KEY", API_KEY, true)
                    .add_header("content-type", MULTIPART_TIPO, true)
                    .body(corpo)
                    .send(&servico)
                    .await,
            )
            .await,
        );
    }

    // O catcher também negocia no 405?
    for aceita in ["application/json", "text/plain"] {
        let caso: &'static str =
            Box::leak(format!("POST /ping (405) com Accept: {aceita}").into_boxed_str());
        medicoes.push(
            medir(
                caso,
                TestClient::post("http://localhost/ping")
                    .add_header("accept", aceita, true)
                    .send(&servico)
                    .await,
            )
            .await,
        );
    }

    // Normalização de barras: qual é exatamente a regra?
    for caminho in ["/ping", "/ping/", "/ping//", "//ping", "/ping///", "/./ping", "/ping/x"] {
        let caso: &'static str = Box::leak(format!("GET {caminho} (barras)").into_boxed_str());
        medicoes.push(
            medir(
                caso,
                TestClient::get(format!("http://localhost{caminho}"))
                    .send(&servico)
                    .await,
            )
            .await,
        );
    }
    for caminho in ["/pdf/", "/pdf//"] {
        let caso: &'static str = Box::leak(format!("POST {caminho} sem chave (barras)").into_boxed_str());
        medicoes.push(
            medir(
                caso,
                TestClient::post(format!("http://localhost{caminho}"))
                    .send(&servico)
                    .await,
            )
            .await,
        );
    }

    // ---------------------------------------------------------------
    // Saída
    // ---------------------------------------------------------------

    println!("salvo: {}\n", env!("CARGO_PKG_VERSION"));
    println!("| caso | status | Content-Type | corpo |");
    println!("|---|---|---|---|");
    for m in &medicoes {
        println!(
            "| {} | {} | `{}` | {} |",
            m.caso,
            m.status,
            m.content_type,
            if m.corpo.is_empty() {
                "*(vazio)*".to_string()
            } else if m.corpo.len() > 120 {
                format!("`{}…` ({} bytes)", &m.corpo[..120].replace('\n', "\\n"), m.corpo.len())
            } else {
                format!("`{}`", m.corpo.replace('\n', "\\n"))
            }
        );
    }

    println!("\n--- corpos completos ---");
    for m in &medicoes {
        println!("\n### {}\nstatus {} · {}\n{:?}", m.caso, m.status, m.content_type, m.corpo);
    }
}
