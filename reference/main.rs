use std::collections::HashSet;
use std::fmt::Debug;
use std::sync::OnceLock;
use std::sync::{
    Arc,
    atomic::{AtomicUsize, Ordering},
};

use dotenvy::dotenv;
use salvo::prelude::*;
use salvo::server::ServerHandle;
use sha2::{Digest, Sha256};
use sqlx::{PgPool, Pool};
use tantivy::collector::TopDocs;
use tantivy::query::QueryParser;
use tantivy::{Index, TantivyDocument};
use tantivy::schema::Value;
use tantivy::schema::{STORED, Schema, TEXT};
use tokio::signal;
use tokio::time::sleep;
use serde::{Deserialize, Serialize};
use salvo::logging::Logger;

const SERVIDOR_IP_PADRAO: &str = "192.168.42.1";
const SERVIDOR_PORTA_PADRAO: u16 = 6001;
const API_KEY: &str = "01956cb2-2f85-7440-9767-1a6651c10e0f";

static DB_POOL: OnceLock<PgPool> = OnceLock::new();

#[tokio::main]
async fn main() {
    dotenv().ok();

    tracing_subscriber::fmt().init();

    let database_url = std::env::var("DATABASE_URL").expect("Variável ambiental DATABASE_URL");

    let pool = make_db_pool(&database_url).await;
    DB_POOL.set(pool).unwrap();

    // Contagem do número de tarefas em andamento
    let tarefas = Arc::new(AtomicUsize::new(0));

    let servidor_ip = std::env::var("SERVIDOR_IP").unwrap_or(String::from(SERVIDOR_IP_PADRAO));
    let servidor_porta = std::env::var("SERVIDOR_PORTA")
        .unwrap_or_default()
        .parse::<u16>()
        .unwrap_or(SERVIDOR_PORTA_PADRAO);

    let servidor = format!("{servidor_ip}:{servidor_porta}");

    let rota_ping = Router::with_path("/ping").get(ping);
    let rota_pdf = Router::with_path("/pdf")
        .hoop(autenticar)
        .hoop(affix_state::inject(tarefas.clone()))
        .post(upload_pdf);

    let acceptor = TcpListener::new(&servidor).bind().await;
    let router = Router::new().hoop(RequestId::new()).push(rota_ping).push(rota_pdf);
    let service = Service::new(router).hoop(Logger::new());
    let server = Server::new(acceptor);
    let handle = server.handle();

    tokio::spawn(listen_shutdown_signal(handle));

    tracing::info!("Servidor iniciando em: {servidor}");

    tokio::spawn(async move {
        server.serve(service).await;

        while tarefas.load(Ordering::SeqCst) > 0 {
            tracing::info!("Aguardando tarefas encerrarem.");
            sleep(tokio::time::Duration::from_millis(100)).await;
        }
    })
    .await
    .unwrap();
}

fn db_pool<'a>() -> &'a PgPool {
    DB_POOL.get().unwrap()
}

async fn make_db_pool(database_url: &str) -> PgPool {
    Pool::connect(database_url).await.unwrap()
}

async fn listen_shutdown_signal(handle: ServerHandle) {
    // Wait Shutdown Signal
    let ctrl_c = async {
        signal::ctrl_c()
            .await
            .expect("failed to install Ctrl+C handler");
    };

    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("failed to install signal handler")
            .recv()
            .await;
    };

    tokio::select! {
        () = ctrl_c => println!(">>>>> <CTRL>-C recebido"),
        () = terminate => println!(">>>>> Kill recebido"),
    };

    // Graceful Shutdown Server
    handle.stop_graceful(None);
}

#[handler]
async fn ping() -> &'static str {
    "pong"
}

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

#[handler]
async fn upload_pdf(depot: &mut Depot, req: &mut Request, res: &mut Response) {
    let id_requisicao = req.header::<String>("x-request-id").unwrap_or_default();

    tracing::info!("[ID Requisição: {id_requisicao} recebida");

    let data_caderno = req.form::<String>("data-caderno").await;
    let data_disponibilizacao = req.form::<String>("data-disponibilizacao").await;

    let id_usuario = req.form::<String>("id-usuario").await;
    let id_caderno = req.form::<String>("id-caderno").await;
    let arquivo = req.file("pdf").await;

    let mut criticas = vec![];

    let mut diario = DiarioPDF {
        ..Default::default()
    };

    let data_caderno = if let Some(v) = data_caderno {
        if let Ok(v) = chrono::NaiveDate::parse_from_str(&v, "%Y-%m-%d") {
            Some(v)
        } else {
            criticas.push("Data do caderno é inválida");
            None
        }
    } else {
        criticas.push("Data do caderno não informada");
        None
    };
    diario.data_caderno = data_caderno;

    let data_disponibilizacao = if let Some(v) = data_disponibilizacao {
        if let Ok(v) = chrono::NaiveDate::parse_from_str(&v, "%Y-%m-%d") {
            Some(v)
        } else {
            criticas.push("Data de disponibilização é inválida");
            None
        }
    } else {
        criticas.push("Data de disponibilização não informada");
        None
    };
    diario.data_disponibilizacao = data_disponibilizacao;

    let id_usuario = if let Some(v) = id_usuario {
        if let Ok(v) = v.parse::<i64>() {
            Some(v)
        } else {
            criticas.push("Id do usuário é inválido");
            None
        }
    } else {
        criticas.push("Id do usuário não informado");
        None
    };
    diario.id_usuario = id_usuario;

    let id_caderno = if let Some(v) = id_caderno {
        if let Ok(v) = v.parse::<i32>() {
            Some(v)
        } else {
            criticas.push("Id do caderno é inválido");
            None
        }
    } else {
        criticas.push("Id do caderno não informado");
        None
    };
    diario.id_caderno = id_caderno;

    let arquivo_pdf = if let Some(v) = arquivo {
        if let Some(v) = v.name() {
            Some(v.to_string())
        } else {
            criticas.push("PDF não possui nome");
            None
        }
    } else {
        criticas.push("PDF não enviado");
        None
    };
    diario.arquivo_pdf = arquivo_pdf;

    tracing::info!("[Requisição: {id_requisicao}] -> Parâmetros recebidos: {:?}", diario);

    if !criticas.is_empty() {
        tracing::error!("[ID Requisição: {id_requisicao}] -> Críticas: {:?}", &criticas);
        let mensagem = criticas.join(",");
        res.status_code(StatusCode::BAD_REQUEST);
        res.render(&mensagem);
        return;
    }

    if arquivo.is_none() {
        res.status_code(StatusCode::BAD_REQUEST);
        res.render("[ID Requisição: {id_requisicao}] -> PDF não enviado");
        return;
    }
    let arquivo = arquivo.unwrap(); // Unwrap seguro

    let conteudo = tokio::fs::read(&arquivo.path()).await.unwrap();
    let hash = calcular_hash(&conteudo);
    diario.hash_sha256 = Some(hash.clone());

    let id_pdf = match registrar_pdf(diario).await {
        Ok(id) => id,
        Err(e) => {
            tracing::error!("Erro ao registrar o PDF: {e:?}");
            res.status_code(StatusCode::UNPROCESSABLE_ENTITY);
            res.render("Erro ao processar o PDF");
            return;
        }
    };

    // Se chegou até aqui, vamos processar o PDF
    
    let _ = atualizar_status_importacao(id_pdf, 0).await;
    
    let tarefas = depot.obtain::<Arc<AtomicUsize>>().unwrap().clone();
    tarefas.fetch_add(1, Ordering::SeqCst);

    let tarefas_clone = tarefas.clone();

    // Executa em thread separada
    tokio::task::spawn(async move {
        tracing::info!("[ID Importação: {id_pdf}] -> Processo de recorte INICIADO");

        let _ = registrar_inicio_importacao(id_pdf).await;
        let _ = atualizar_status_importacao(id_pdf, 1).await;

        scopeguard::defer! {
            tracing::info!("[ID Importação: {id_pdf}] -> Processo de recorte FINALIZADO");
            tarefas_clone.fetch_sub(1, Ordering::SeqCst);
        };
        
        let _ = atualizar_status_importacao(id_pdf, 2).await;

        if let Ok(idx) = criar_indice(id_pdf, &conteudo) {

            let _ = atualizar_status_importacao(id_pdf, 3).await;

            if let Ok(chaves) = obter_chaves_pesquisa(id_pdf).await {

                tracing::info!("[ID Importação: {id_pdf}] -> Há {} perfis/expressões a pesquisar", chaves.len());

                let mut nr_recortes = 0;
                let mut pages = HashSet::new();
                let mut id_perfil: i64 = 0;

                for chave in chaves {
                    if let Ok(mut recortes) = recortar(&idx, &chave.expressao_nm) {
                        // Ordena os recortes
                        recortes.sort_by(|a, b| a.page.cmp(&b.page));

                        if id_perfil != chave.id_perfil {
                            pages = HashSet::new();
                            id_perfil = chave.id_perfil;
                        }
                        if !recortes.is_empty() {
                            tracing::info!("[ID Importação: {id_pdf}] -> Há {} recorte(s) a filtrar para [Perfil {} - Expressão {}]", recortes.len(),&chave.id_perfil, &chave.expressao_nm);
                        }

                        // Elimina duplicados
                        let mut recortes_filtrados = vec![];

                        for recorte in recortes {
                            let pagina = recorte.page;
                            if !pages.contains(&pagina) {
                                recortes_filtrados.push(recorte.clone());
                            }
                            pages.insert(pagina);
                        }

                        nr_recortes += recortes_filtrados.len();

                        if !recortes_filtrados.is_empty() {
                            tracing::info!("[ID Importação: {id_pdf}] -> Há {} recorte(s) a registrar para [Perfil {} - Expressão {}]", recortes_filtrados.len(),&chave.id_perfil, &chave.expressao_nm);
                            if let Ok(contagem) = salvar_recorte(id_pdf, &chave, recortes_filtrados).await {
                                tracing::info!("[ID Importação: {id_pdf}] -> Registrado(s) {} recorte(s) para [Perfil {} - Expressão {}]", contagem, &chave.id_perfil, &chave.expressao_nm);
                            } else { 
                                tracing::error!("[ID Importação: {id_pdf} -> Erro ao registrar recorte para [Perfil {} -Expressão {}]", &chave.id_perfil, &chave.expressao_nm);
                                let _ = atualizar_status_importacao(id_pdf, -1).await;
                               return;
                            }
                        }
                    } else {
                        tracing::error!("[ID Importação: {id_pdf}] -> Erro a recortar [Perfil {} - Expressão {}]", &chave.id_perfil, &chave.expressao_nm);
                        let _ = atualizar_status_importacao(id_pdf, -1).await;
                        return;
                    }
                }
                // Chegou aqui sem erros, indica finalização do processo
                let _ = atualizar_status_importacao(id_pdf, 5).await;
                let _ = registrar_termino_importacao(id_pdf, nr_recortes).await;
            } else {
                tracing::error!("[ID Importação: {id_pdf}] -> Erro ao obter perfis/expressões");
                let _ = atualizar_status_importacao(id_pdf, -1).await;
                // Return desnecessário aqui! Estamos em uma thread separada.
            }
        } else {
            tracing::error!("[ID Importação: {id_pdf} - > Erro ao criar índice de buscas");
            let _ = atualizar_status_importacao(id_pdf, -1).await;
            // Return desnecessário aqui. Estamos em uma thread separada.
        }
    });

    res.status_code(StatusCode::OK);
    res.render("PDF carregado com sucesso");
}
fn calcular_hash(arquivo_pdf: &[u8]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(arquivo_pdf);
    hex::encode(hasher.finalize())
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct ResultadoBusca {
    pub page: u64,
    pub text: String,
}

#[derive(Debug,Clone,Default,Serialize)]
pub struct Recorte {
    pub page: u64,
    pub text: String,
    pub highlight: String,
}

fn recortar(idx: &Index, key:&str) -> anyhow::Result<Vec<Recorte>> {

    let mut recortes:Vec<Recorte> = vec![];

    let mut sch = Schema::builder();

    sch.add_u64_field("page", STORED);
    sch.add_text_field("text", TEXT | STORED);

    let sch = sch.build();

    let page_field = sch.get_field("page")?;
    let text_field = sch.get_field("text")?;

    let query = QueryParser::for_index(idx, vec![text_field]).parse_query(&format!(r#""{key}""#))?;

    let reader = idx
        .reader_builder()
        .reload_policy(tantivy::ReloadPolicy::OnCommitWithDelay)
        .try_into()?;

    let searcher = reader.searcher();

    let top_docs = searcher
        .search(&query, &TopDocs::with_limit(1_000_000))?;

    for (_score, doc_address) in top_docs {
        let doc = searcher.doc::<TantivyDocument>(doc_address)?;
        let texto = doc.get_first(text_field).unwrap().as_str().unwrap().to_string();

        // TODO: Verificar se isto está correto!!!
        if key.contains('&') {
               let exp = format!("(?imx){}", key.replace('&', r"\s*&\s*"));
               let re = regex::Regex::new(&exp).unwrap();
               if !re.is_match(&texto) {
                   continue;
               }
        }

        recortes.push(
              Recorte {
                page: doc.get_first(page_field).unwrap().as_u64().unwrap(),
                text: doc.get_first(text_field).unwrap().as_str().unwrap().to_string(),
                highlight: texto,
              }  
        );        
    }
    
    Ok(recortes)
}

#[derive(sqlx::FromRow, Debug)]
pub struct DiarioPDF {
    #[sqlx(rename = "id_inclusao")]
    pub id_usuario: Option<i64>,
    #[sqlx(rename = "id_cadernos")]
    pub id_caderno: Option<i32>,
    #[sqlx(rename = "data_caderno")]
    pub data_caderno: Option<chrono::NaiveDate>,
    #[sqlx(rename = "data_disponibilizacao")]
    pub data_disponibilizacao: Option<chrono::NaiveDate>,
    #[sqlx(rename = "status")]
    pub status: Option<i32>,
    #[sqlx(rename = "nome_original_pdf")]
    pub arquivo_pdf: Option<String>,
    #[sqlx(rename = "tipo_caderno")]
    pub tipo_caderno: Option<String>,
    #[sqlx(rename = "hash")]
    pub hash_sha256: Option<String>,
}

impl Default for DiarioPDF {
    fn default() -> Self {
        Self {
            id_usuario: Option::default(),
            id_caderno: Option::default(),
            data_caderno: Option::default(),
            data_disponibilizacao: Option::default(),
            status: Some(0),
            arquivo_pdf: Option::default(),
            tipo_caderno: Some(String::from("PDF")),
            hash_sha256: Option::default(),
        }
    }
}

async fn registrar_pdf(diario: DiarioPDF) -> Result<i64, sqlx::Error> {
    let query = r"
        INSERT INTO
            recorte.tb_importacao (
                id_inclusao,
                id_cadernos,
                data_caderno,
                data_disponibilizacao,
                status,
                nome_original_pdf,
                tipo_caderno,
                hash
            )
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8 )
        RETURNING id_importacao
    ";

    let id: (i64,) = sqlx::query_as(query)
        .bind(diario.id_usuario)
        .bind(diario.id_caderno)
        .bind(diario.data_caderno)
        .bind(diario.data_disponibilizacao)
        .bind(diario.status)
        .bind(diario.arquivo_pdf)
        .bind(diario.tipo_caderno)
        .bind(diario.hash_sha256)
        .fetch_one(db_pool())
        .await?;

    Ok(id.0)
}

fn criar_indice(id_pdf: i64, conteudo: &[u8]) -> anyhow::Result<tantivy::Index> {
    tracing::info!("[ID Importação: {id_pdf}] -> Paginando PDF");

    let documento = mupdf::Document::from_bytes(conteudo, "pdf")?;

    let mut paginas = vec![];

    let re_hifen_final_de_linha =
        regex::Regex::new(r#"(?imx)(\w+)(-\n)"#).expect("Expressão regular para remoção de hífens");

    for page in documento.pages()? {
        let text_page = page?.to_text_page(mupdf::text_page::TextPageOptions::empty())?;
        let mut pagina = vec![];
        for block in text_page.blocks() {
            for line in block.lines() {
                let chars = format!("{}\n", line
                    .chars()
                    .map(|c| c.char().unwrap_or(' '))
                    .collect::<String>().trim());
                let chars = re_hifen_final_de_linha
                    .replace_all(&chars, "$1")
                    .to_string();
                let chars = diacritics::remove_diacritics(&chars);
                pagina.push(chars);
            }
        }
        paginas.push(pagina.join(""));
    }

    tracing::info!( "[ID Importação: {id_pdf}] -> Há {} página(s) a indexar", documento.page_count().unwrap_or(0) );

    let mut sch = Schema::builder();

    sch.add_u64_field("page", STORED);
    sch.add_text_field("text", TEXT | STORED);

    let sch = sch.build();

    let idx = tantivy::Index::create_in_ram(sch.clone());

    let mut idx_writer = idx.writer(500_000_000)?;

    let page_field = sch.get_field("page")?;
    let text_field = sch.get_field("text")?;

    for (i, pagina) in paginas.iter().enumerate() {
        let mut doc = TantivyDocument::default();
        doc.add_u64(page_field, (i + 1) as u64);
        doc.add_text(text_field, pagina);
        idx_writer.add_document(doc)?;
    }

    idx_writer.commit()?;

    tracing::info!("[ID Importação: {id_pdf}] -> PDF indexado");

    Ok(idx)
}

#[derive(sqlx::FromRow, Debug, Clone, Default)]
pub struct ChavePesquisa {
    pub id_perfil: i64,
    pub expressao_nm: String,
}

async fn obter_chaves_pesquisa(id_pdf: i64) -> anyhow::Result<Vec<ChavePesquisa>> {
    let query = "
        SELECT DISTINCT
            tp.id_perfil,
            tpv.expressao_nm
        FROM
            recorte.tb_perfil_variacao tpv 
            INNER JOIN recorte.tb_perfil tp ON tpv.id_perfil = tp.id_perfil
            INNER JOIN recorte.tb_perfil_caderno ec ON tp.id_perfil = ec.id_perfil
            INNER JOIN recorte.tb_cliente tc ON tc.id_cliente =  tp.id_cliente
            INNER JOIN recorte.tb_importacao ti ON ti.id_cadernos = ec.id_cadernos
        WHERE
            tpv.expressao_nm IS NOT NULL
            AND ec.status = 'S'
            AND tc.status = 'A'
            AND ti.id_importacao = $1
        ORDER BY
            tp.id_perfil,
            tpv.expressao_nm 
    ";

    let chaves = sqlx::query_as::<_, ChavePesquisa>(query)
        .bind(id_pdf)
        .fetch_all(db_pool())
        .await?;

    Ok(chaves)
}

async fn salvar_recorte(id_pdf: i64, chave: &ChavePesquisa, recortes:Vec<Recorte>) -> anyhow::Result<i32> {
    let query_recorte = "
        INSERT INTO
            recorte.tb_recorte(
                id_importacao,
                nr_pagina,
                id_perfil,
                expressao_busca,
                dt_recorte
            )
        VALUES ($1, $2, $3, $4, current_timestamp)
        RETURNING id_recorte
    ";

    let query_recorte_texto = "
        INSERT INTO
            recorte.tb_recorte_texto(
                id_recorte,
                origem,
                recorte
            )
        VALUES ($1, 'PDF', $2)
    ";

    let mut contador=0;

    // TODO: Implementar controle de transação?
    for recorte in recortes {
        let id_recorte: (i64,) = match sqlx::query_as(query_recorte)
            .bind(id_pdf)
            .bind(i64::try_from(recorte.page)?)
            .bind(chave.id_perfil)
            .bind(&chave.expressao_nm)
            .fetch_one(db_pool())
            .await {
                Ok(id) => id,
                Err(e) => {
                    let msg = format!("Erro ao salvar recorte: {e:?}");
                    tracing::error!("{msg}");
                    anyhow::bail!(msg);
                },
            };
        let id_recorte = id_recorte.0;
        
       if let Err(e) = sqlx::query(query_recorte_texto)
        .bind(id_recorte)
        .bind(&recorte.highlight)
        .execute(db_pool())
        .await {
                let msg = format!("Erro ao salvar texto do recorte: {e:?}");
                tracing::error!("{msg}");
                anyhow::bail!(msg);
        };

        contador += 1;
    }

    Ok(contador)
}
/*
enum StatusImportacao {
    Erro,
    Recebido,
    Processando,
    Indexando,
    Recortando,
    Finalizado
}

impl std::convert::From<i32> for StatusImportacao {
    fn from(value: i32) -> Self {
        match value {
            
        }
    }
}
*/

async fn atualizar_status_importacao(id_pdf: i64, status: i32) -> anyhow::Result<()> {
    /*
        ----------------------------
        >>>> Status
        ----------------------------
        -1 - Erro no processo
         0 - Uploaded
         1 - Selecionado para processamento
         2 - Indexando
         3 - Recortando
         4 - Não usado
         5 - Processo finalizado
    */
    let query = "
        UPDATE
            recorte.tb_importacao
        SET
            status = $1
        WHERE
            id_importacao = $2
    ";

        sqlx::query(query)
            .bind(status)
            .bind(id_pdf)
            .execute(db_pool())
            .await?;

    Ok(())
}

async fn registrar_inicio_importacao(id_pdf: i64) -> anyhow::Result<()> {
    let query = "
        UPDATE
            recorte.tb_importacao
        SET
            data_inicio =  current_timestamp
        WHERE
            id_importacao = $1
    ";

    sqlx::query(query)
        .bind(id_pdf)
        .execute(db_pool())
        .await?;

    Ok(())
}

async fn registrar_termino_importacao(id_pdf: i64,nr_recortes:usize) -> anyhow::Result<()> {
    let query = "
        UPDATE
            recorte.tb_importacao
        SET
            data_fim =  current_timestamp,
            total_recortes = $1
        WHERE
            id_importacao = $2
    ";

    sqlx::query(query)
        .bind(i32::try_from(nr_recortes)?)
        .bind(id_pdf)
        .execute(db_pool())
        .await?;

    Ok(())
}
/*
async fn notificar_por_telegram(msg:String) {
    let telegram_api_key = std::env::var("TELEGRAM_API_KEY").unwrap_or_default();
    let telegram_user_id = std::env::var("TELEGRAM_USER_ID").unwrap_or_default();
    let url = format!(
        "https://api.telegram.org/bot{}/sendMessage?chat_id={}&text={}",
        config.telegram_api_key, config.telegram_user_id, mensagem.texto,
    );
}
*/


