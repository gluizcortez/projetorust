package httpapi_test

// Corpos MEDIDOS do catcher do Salvo, capturados por `tools/sonda-http` contra
// o roteador reconstruído de `reference/main.rs:52-60`.
//
// Estão aqui como literais, e não gerados a partir do código sob teste, porque
// derivá-los da própria implementação não provaria nada: o oráculo tem de vir
// de fora.
//
// ⚠ Medidos contra salvo 0.95.2. A versão de produção é a decisão aberta D-15,
// e o HTML do catcher não é contrato estável entre versões do Salvo — status,
// Content-Type e negociação de conteúdo são. Ver D-20.

const catcher404HTML = `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width">
    <title>404: Not Found</title>
    <style>
    :root {
        --bg-color: #fff;
        --text-color: #222;
    }
    body {
        background: var(--bg-color);
        color: var(--text-color);
        text-align: center;
    }
    pre { text-align: left; padding: 0 1rem; }
    footer{text-align:center;}
    @media (prefers-color-scheme: dark) {
        :root {
            --bg-color: #222;
            --text-color: #ddd;
        }
        a:link { color: red; }
        a:visited { color: #a8aeff; }
        a:hover {color: #a8aeff;}
        a:active {color: #a8aeff;}
    }
    </style>
</head>
<body>
    <div><h1>404: Not Found</h1><h3>The requested resource could not be found.</h3><hr><footer><a href="https://salvo.rs" target="_blank">salvo</a></footer></div>
</body>
</html>`

const catcher405HTML = `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width">
    <title>405: Method Not Allowed</title>
    <style>
    :root {
        --bg-color: #fff;
        --text-color: #222;
    }
    body {
        background: var(--bg-color);
        color: var(--text-color);
        text-align: center;
    }
    pre { text-align: left; padding: 0 1rem; }
    footer{text-align:center;}
    @media (prefers-color-scheme: dark) {
        :root {
            --bg-color: #222;
            --text-color: #ddd;
        }
        a:link { color: red; }
        a:visited { color: #a8aeff; }
        a:hover {color: #a8aeff;}
        a:active {color: #a8aeff;}
    }
    </style>
</head>
<body>
    <div><h1>405: Method Not Allowed</h1><h3>The request method is not supported for the requested resource.</h3><hr><footer><a href="https://salvo.rs" target="_blank">salvo</a></footer></div>
</body>
</html>`

// Corpos das outras formas negociadas, também medidos.
const (
	catcher404JSON  = `{"error":{"code":404,"name":"Not Found","brief":"The requested resource could not be found."}}`
	catcher404Texto = "code: 404\n\nname: Not Found\n\nbrief: The requested resource could not be found."
	catcher404XML   = `<?xml version="1.0" encoding="UTF-8"?><Data><code>404</code><name>Not Found</name><brief>The requested resource could not be found.</brief></Data>`

	catcher405JSON  = `{"error":{"code":405,"name":"Method Not Allowed","brief":"The request method is not supported for the requested resource."}}`
	catcher405Texto = "code: 405\n\nname: Method Not Allowed\n\nbrief: The request method is not supported for the requested resource."
)
